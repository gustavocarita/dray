package job

import (
	"bufio"
	"bytes"
	"io"
	"strings"

	log "github.com/Sirupsen/logrus"
)

const (
	BeginDelimiter = "----BEGIN PANAMAX DATA----"
	EndDelimiter   = "----END PANAMAX DATA----"
	EOT            = byte('\u0003')
)

var (
	accessor         JobAccessor
	containerFactory ContainerFactory
)

func init() {
	accessor = &redisJobAccessor{}
	containerFactory = &dockerContainerFactory{}
}

type Job struct {
	ID             string    `json:"id,omitempty"`
	Name           string    `json:"name,omitempty"`
	Steps          []JobStep `json:"steps,omitempty"`
	StepsCompleted string    `json:"stepsCompleted,omitempty"`
}

type JobStep struct {
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
}

type JobLog struct {
	Index int      `json:"index,omitempty"`
	Lines []string `json:"lines"`
}

func ListAll() ([]Job, error) {
	return accessor.All()
}

func GetByID(jobID string) (*Job, error) {
	return accessor.Get(jobID)
}

func (job *Job) Create() error {
	return accessor.Create(job)
}

func (job *Job) Delete() error {
	return accessor.Delete(job.ID)
}

func (job *Job) GetLog(index int) (*JobLog, error) {
	return accessor.GetJobLog(job.ID, index)
}

func (job *Job) Execute() error {
	var capture io.Reader

	for i := range job.Steps {
		capture, _ = job.executeStep(i, capture)
		accessor.CompleteStep(job.ID)
	}
	return nil
}

func (job *Job) executeStep(stepIndex int, stdIn io.Reader) (io.Reader, error) {
	stdOut := &bytes.Buffer{}
	stdErr := &bytes.Buffer{}
	step := job.Steps[stepIndex]
	container := containerFactory.NewContainer(step.Source)

	err := container.Create()
	if err != nil {
		return nil, err
	}
	log.Debugf("Container %s created", container)

	go func() {
		container.Attach(stdIn, stdOut, stdErr)
		stdOut.Write([]byte{EOT, '\n'})
	}()

	err = container.Start()
	if err != nil {
		return nil, err
	}
	log.Debugf("Container %s started", container)

	output, err := job.captureOutput(stdOut)
	log.Debugf("Container %s stopped", container)

	err = container.Remove()
	if err != nil {
		return nil, err
	}
	log.Debugf("Container %s removed", container)

	return output, nil
}

func (job *Job) captureOutput(r io.Reader) (io.Reader, error) {
	reader := bufio.NewReader(r)
	buffer := &bytes.Buffer{}
	capture := false

	for {
		line, _ := reader.ReadBytes('\n')

		if len(line) > 0 {
			if line[0] == EOT {
				break
			}
			s := strings.TrimSpace(string(line))
			log.Debugf(s)
			accessor.AppendLogLine(job.ID, s)

			if s == EndDelimiter {
				capture = false
			}

			if capture {
				buffer.WriteString(s + "\n")
			}

			if s == BeginDelimiter {
				capture = true
			}
		}
	}

	return buffer, nil
}