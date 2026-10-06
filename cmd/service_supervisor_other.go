//go:build !darwin

package cmd

func runServerSupervisor()                    {}
func restartSupervisedProcess() (bool, error) { return false, nil }
