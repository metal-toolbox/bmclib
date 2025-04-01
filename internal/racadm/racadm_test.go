package racadm

import (
	"context"
	"os"
	"testing"

	ex "github.com/metal-toolbox/bmclib/internal/executor"
)

func newFakeRacadm(t *testing.T, fixtureName string) *Racadm {
	e := &Racadm{
		Executor: ex.NewFakeExecutor("racadm"),
	}

	b, err := os.ReadFile("../../fixtures/internal/racadm/" + fixtureName)
	if err != nil {
		t.Error(err)
	}

	e.Executor.SetStdout(b)

	return e
}

func TestExec_Run(t *testing.T) {
	// Create a new instance of Sum
	exec := newFakeRacadm(t, "SetBiosConfigFromFile")

	// Create a new context
	ctx := context.Background()

	// Call the run function
	_, err := exec.run(ctx, "SetBiosConfigFromFile")

	// Check the output and error
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}
func TestParsePercentComplete(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      int
		expectedError bool
	}{
		{
			name: "ValidPercentComplete",
			input: `---------------------------- JOB -------------------------
[Job ID=JID_000123456789]
Job Name=Configure: Import Server Configuration Profile
Status=Running
Scheduled Start Time=[Not Applicable]
Expiration Time=[Not Applicable]
Actual Start Time=[Thu, 27 Mar 2025 16:44:19]
Actual Completion Time=[Not Applicable]
Message=[SYS058: Applying configuration changes.]
Percent Complete=[20]
----------------------------------------------------------`,
			expected:      20,
			expectedError: false,
		},
		{
			name: "MissingPercentComplete",
			input: `---------------------------- JOB -------------------------
[Job ID=JID_000123456789]
Job Name=Configure: Import Server Configuration Profile
Status=Running
Scheduled Start Time=[Not Applicable]
Expiration Time=[Not Applicable]
Actual Start Time=[Thu, 27 Mar 2025 16:44:19]
Actual Completion Time=[Not Applicable]
Message=[SYS058: Applying configuration changes.]
----------------------------------------------------------`,
			expected:      0,
			expectedError: true,
		},
		{
			name: "InvalidPercentCompleteFormat",
			input: `---------------------------- JOB -------------------------
[Job ID=JID_000123456789]
Job Name=Configure: Import Server Configuration Profile
Status=Running
Scheduled Start Time=[Not Applicable]
Expiration Time=[Not Applicable]
Actual Start Time=[Thu, 27 Mar 2025 16:44:19]
Actual Completion Time=[Not Applicable]
Message=[SYS058: Applying configuration changes.]
Percent Complete=[invalid]
----------------------------------------------------------`,
			expected:      0,
			expectedError: true,
		},
		{
			name:          "EmptyInput",
			input:         ``,
			expected:      0,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePercentComplete(tt.input)
			if (err != nil) != tt.expectedError {
				t.Errorf("Expected error: %v, got: %v", tt.expectedError, err)
			}
			if result != tt.expected {
				t.Errorf("Expected result: %d, got: %d", tt.expected, result)
			}
		})
	}
}
func TestParseJobId(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      string
		expectedError bool
	}{
		{
			name: "ValidJobId",
			input: `Please wait while racadm transfers the file.
File transferred successfully. Initiating the import operation.
RAC977: Import configuration XML file operation initiated.
Use the "racadm jobqueue view -i JID_000123456789" command to view the status
of the operation.`,
			expected:      "JID_000123456789",
			expectedError: false,
		},
		{
			name: "MissingJobId",
			input: `Please wait while racadm transfers the file.
File transferred successfully. Initiating the import operation.
RAC977: Import configuration XML file operation initiated.`,
			expected:      "",
			expectedError: true,
		},
		{
			name: "InvalidJobIdFormat",
			input: `Please wait while racadm transfers the file.
File transferred successfully. Initiating the import operation.
RAC977: Import configuration XML file operation initiated.
Use the "racadm jobqueue view -i INVALID_JOB_ID" command to view the status
of the operation.`,
			expected:      "",
			expectedError: true,
		},
		{
			name:          "EmptyInput",
			input:         ``,
			expected:      "",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseJobId(tt.input)
			if (err != nil) != tt.expectedError {
				t.Errorf("Expected error: %v, got: %v", tt.expectedError, err)
			}
			if result != tt.expected {
				t.Errorf("Expected result: %s, got: %s", tt.expected, result)
			}
		})
	}
}
