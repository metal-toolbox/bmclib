package racadm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	ex "github.com/metal-toolbox/bmclib/internal/executor"

	"github.com/go-logr/logr"
)

const (
	// tickerInterval is the interval for the ticker
	tickerInterval = 30 * time.Second
	// timeout is the timeout for the job queue
	timeout = (14 * time.Minute) + (30 * time.Second)
	// maxErrors is the maximum number of errors before failing
	maxErrors = 3
)

type Racadm struct {
	Executor   ex.Executor
	RacadmPath string
	Log        logr.Logger
	Host       string
	Username   string
	Password   string
}

type Option func(*Racadm)

func WithRacadmPath(racadmPath string) Option {
	return func(c *Racadm) {
		c.RacadmPath = racadmPath
	}
}

func WithLogger(log logr.Logger) Option {
	return func(c *Racadm) {
		c.Log = log
	}
}

func New(host, user, pass string, opts ...Option) (*Racadm, error) {
	racadm := &Racadm{
		Host:     host,
		Username: user,
		Password: pass,
		Log:      logr.Discard(),
	}

	for _, opt := range opts {
		opt(racadm)
	}

	var err error

	if racadm.RacadmPath == "" {
		racadm.RacadmPath, err = exec.LookPath("racadm")
		if err != nil {
			return nil, err
		}
	} else {
		if _, err = os.Stat(racadm.RacadmPath); err != nil {
			return nil, err
		}
	}

	e := ex.NewExecutor(racadm.RacadmPath)
	e.SetEnv([]string{"LC_ALL=C.UTF-8"})
	racadm.Executor = e

	return racadm, nil
}

// Open a connection to a BMC
func (c *Racadm) Open(ctx context.Context) (err error) {
	return nil
}

// Close a connection to a BMC
func (c *Racadm) Close(ctx context.Context) (err error) {
	return nil
}

func (s *Racadm) run(ctx context.Context, command string, additionalArgs ...string) (output string, err error) {
	racadmArgs := []string{"-r", s.Host, "-u", s.Username, "-p", s.Password, "--nocertwarn", command}
	racadmArgs = append(racadmArgs, additionalArgs...)

	s.Log.V(9).WithValues(
		"racadmArgs",
		racadmArgs,
	).Info("Calling racadm")

	s.Executor.SetArgs(racadmArgs)

	result, err := s.Executor.ExecWithContext(ctx)
	if result == nil {
		return "", err
	}
	if err != nil {
		return string(result.Stderr), err
	}

	return string(result.Stdout), err
}

func (s *Racadm) ChangeBiosCfg(ctx context.Context, cfgFile string) (err error) {
	args := []string{"-t", "xml", "-f", cfgFile}

	// check if there is enough time left in the context
	d, _ := ctx.Deadline()
	if time.Until(d) < timeout {
		return errors.New("remaining context deadline (minimum: " + timeout.String() + ") insufficient to perform update, remaining: " + time.Until(d).String())
	}

	output, err := s.run(ctx, "set", args...)
	if err != nil {
		return fmt.Errorf("failed to execute racadm set command: %w", err)
	}

	jobID, err := parseJobId(output)
	if err != nil {
		return fmt.Errorf("failed to parse JobID: %w", err)
	}

	s.Log.V(9).WithValues("jobID", jobID).Info("JobID created")

	// Wait for the job to complete with a timeout
	timeout := time.After(timeout)
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	errorCount := 0

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled while waiting for job completion: %w", ctx.Err())
		case <-timeout:
			return fmt.Errorf("timeout exceeded while waiting for job to complete")
		case <-ticker.C:
			output, err := s.GetJobQueue(ctx, jobID)
			if err != nil {
				errorCount++
				s.Log.Error(err, "failed to get job queue, retrying", "errorCount", errorCount)
				if errorCount >= maxErrors {
					return fmt.Errorf("exceeded maximum consecutive errors while waiting for job completion: %w", err)
				}
				continue
			}

			percentComplete, err := parsePercentComplete(output)
			if err != nil {
				errorCount++
				s.Log.Error(err, "failed to parse percent complete, retrying", "errorCount", errorCount)
				if errorCount >= maxErrors {
					return fmt.Errorf("exceeded maximum consecutive errors while parsing percent complete: %w", err)
				}
				continue
			}

			// Reset error count on successful read
			errorCount = 0

			s.Log.V(9).WithValues("percentComplete", percentComplete).Info("Job progress update")

			if percentComplete == 100 {
				s.Log.Info("Job completed successfully")
				return nil
			}
		}
	}
}

func (s *Racadm) SetBiosConfigurationFromFile(ctx context.Context, cfg string) (err error) {

	// Open tmp file to hold cfg
	inputConfigTmpFile, err := os.CreateTemp("", "bmclib")
	if err != nil {
		return err
	}

	defer os.Remove(inputConfigTmpFile.Name())

	_, err = inputConfigTmpFile.WriteString(cfg)
	if err != nil {
		return err
	}

	err = inputConfigTmpFile.Close()
	if err != nil {
		return err
	}

	return s.ChangeBiosCfg(ctx, inputConfigTmpFile.Name())
}

func (s *Racadm) GetJobQueue(ctx context.Context, jobID string) (output string, err error) {
	output, err = s.run(ctx, "jobqueue", "view", "-i", jobID)
	if err != nil {
		return "", err
	}

	return output, nil
}

// Parse out the JobID from the job creation message. Example message:
//
//	Please wait while racadm transfers the file.
//	File transferred successfully. Initiating the import operation.
//	RAC977: Import configuration XML file operation initiated.
//	Use the "racadm jobqueue view -i JID_000123456789" command to view the status
//	of the operation.
func parseJobId(message string) (jobId string, err error) {
	for _, line := range strings.Split(message, "\n") {
		if strings.Contains(line, "JID_") {
			// job id is in the format JID_XXXXXXXXXXXX and is 16 chars long
			if idx := strings.Index(line, "JID_"); idx != -1 {
				jobId = line[idx : idx+16]
				return jobId, nil
			}
		}
	}
	return "", fmt.Errorf("failed to find JobID in output")
}

func parsePercentComplete(message string) (percentComplete int, err error) {
	// Example of in-progress message:
	//
	//	---------------------------- JOB -------------------------
	//	[Job ID=JID_000123456789]
	//	Job Name=Configure: Import Server Configuration Profile
	//	Status=Running
	//	Scheduled Start Time=[Not Applicable]
	//	Expiration Time=[Not Applicable]
	//	Actual Start Time=[Thu, 27 Mar 2025 16:44:19]
	//	Actual Completion Time=[Not Applicable]
	//	Message=[SYS058: Applying configuration changes.]
	//	Percent Complete=[20]
	//	----------------------------------------------------------
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Percent Complete=[") {
			re := regexp.MustCompile(`Percent Complete=\[(\d+)\]`)
			matches := re.FindStringSubmatch(line)
			if len(matches) != 2 {
				return 0, fmt.Errorf("failed to extract Percent Complete using regex: %s", line)
			}

			percentComplete, err := strconv.Atoi(matches[1])
			if err != nil {
				return 0, fmt.Errorf("failed to parse percent complete: %w", err)
			}
			return percentComplete, nil
		}
	}
	return 0, fmt.Errorf("failed to find Percent Complete in output")
}
