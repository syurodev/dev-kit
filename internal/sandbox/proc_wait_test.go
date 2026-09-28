package sandbox

import (
	"os/exec"
	"sync"
	"testing"
)

func TestProcConcurrentWaitersObserveSameExit(t *testing.T) {
	command, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("requires sh")
	}
	cmd := exec.Command(command, "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := NewProc(cmd)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.Wait()
			if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 7 {
				t.Errorf("Wait = %v", err)
			}
		}()
	}
	wg.Wait()
}
