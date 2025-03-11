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
