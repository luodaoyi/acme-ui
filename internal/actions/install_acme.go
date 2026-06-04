package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const AcmeInstallURL = "https://get.acme.sh"

type InstallAcmeRequest struct {
	Email   string `json:"email"`
	Confirm bool   `json:"confirm"`
}

type InstallAcmePlan struct {
	Email   string
	Preview string
}

func BuildInstallAcme(req InstallAcmeRequest) (InstallAcmePlan, error) {
	if !req.Confirm {
		return InstallAcmePlan{}, errors.New("install confirmation is required")
	}
	email := strings.TrimSpace(req.Email)
	if email != "" && !emailPattern.MatchString(email) {
		return InstallAcmePlan{}, errors.New("valid email is required")
	}
	preview := "curl -fsSL " + AcmeInstallURL + " -o <tmp>/get.acme.sh; sh <tmp>/get.acme.sh"
	if email != "" {
		preview += " email=" + email
	}
	return InstallAcmePlan{
		Email:   email,
		Preview: preview,
	}, nil
}

func RunInstallAcme(ctx context.Context, plan InstallAcmePlan, emit func(stream, text string)) (int, error) {
	tmp, err := os.MkdirTemp("", "acme-ui-install-*")
	if err != nil {
		return -1, err
	}
	defer os.RemoveAll(tmp)

	script := tmp + "/get.acme.sh"
	if code, err := runLogged(ctx, emit, "curl", "-fsSL", AcmeInstallURL, "-o", script); err != nil {
		return code, err
	}
	args := []string{script}
	if plan.Email != "" {
		args = append(args, "email="+plan.Email)
	}
	if code, err := runLogged(ctx, emit, "sh", args...); err != nil {
		return code, err
	}
	emit("stdout", "acme.sh install finished")
	return 0, nil
}

func runLogged(ctx context.Context, emit func(stream, text string), name string, args ...string) (int, error) {
	emit("stdout", "$ "+name+" "+strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stdout.Len() > 0 {
		for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				emit("stdout", line)
			}
		}
	}
	if stderr.Len() > 0 {
		for _, line := range strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				emit("stderr", line)
			}
		}
	}
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return -1, fmt.Errorf("%s: %w", name, err)
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
