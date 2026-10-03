//go:build !unix && !windows

package agent

func LockAgentInstance() (func(), error) {
	return func() {}, nil
}
