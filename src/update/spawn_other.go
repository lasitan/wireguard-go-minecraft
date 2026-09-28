//go:build !unix && !windows

package update

import "errors"

func ServiceManaged() bool { return false }

func spawnDetached(string, []string) (string, error) {
	return "", errors.New("此平台不支持远程升级")
}
