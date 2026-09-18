package codegraph

import "context"

// OpenProject opens projectPath against whatever store the Service already
// holds — the test shape of OpenProjectAt, which production never needs:
// every real open names its per-project index file. Defined here, in a test
// file, so it cannot be called from production code by accident.
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	run, err := s.beginOpen("", projectPath)
	if err != nil {
		return err
	}
	return run(ctx)
}

// SetProjectPath swaps in a fresh store at dbPath without opening anything,
// as OpenProjectAt does before its open.
func (s *Service) SetProjectPath(dbPath string) error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.stopWatcher()
	return s.setProjectPath(dbPath)
}
