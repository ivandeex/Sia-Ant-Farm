package ant

// jobGeneric unlocks the wallet and waits for ants to sync.
func (j *JobRunner) jobGeneric() {
	err := j.StaticTG.Add()
	if err != nil {
		j.staticLogger.Errorf("%v: can't add thread group: %v", j.staticDataDir, err)
		return
	}
	defer j.StaticTG.Done()

	// Wait for ants to be synced
	synced := j.waitForAntsSync()
	if !synced {
		j.staticLogger.Errorf("%v: waiting for ants to sync failed", j.staticDataDir)
		return
	}
}
