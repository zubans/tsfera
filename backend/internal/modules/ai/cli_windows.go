//go:build windows

package ai

import "os/exec"

func setProcessGroup(*exec.Cmd) {}
