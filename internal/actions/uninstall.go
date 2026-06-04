package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/acme-ui/acme-ui/internal/acme"
)

type UninstallRequest struct {
	Paths         []string `json:"paths"`
	Service       string   `json:"service"`
	ReloadService bool     `json:"reloadService"`
	Confirm       string   `json:"confirm"`
}

type UninstallPlan struct {
	Paths         []string
	Service       string
	ReloadService bool
	Preview       string
}

func BuildUninstall(req UninstallRequest) (UninstallPlan, error) {
	if strings.TrimSpace(req.Confirm) != "DELETE" {
		return UninstallPlan{}, errors.New("confirm must be DELETE")
	}
	var paths []string
	seen := make(map[string]bool)
	for _, item := range req.Paths {
		for _, raw := range strings.FieldsFunc(item, func(r rune) bool {
			return r == '\n' || r == '\r' || r == '\t' || r == ','
		}) {
			cleaned, err := acme.NormalizeAbsPath(raw)
			if err != nil {
				return UninstallPlan{}, err
			}
			if !seen[cleaned] {
				seen[cleaned] = true
				paths = append(paths, cleaned)
			}
		}
	}
	if len(paths) == 0 {
		return UninstallPlan{}, errors.New("at least one file path is required")
	}
	service := strings.ToLower(strings.TrimSpace(req.Service))
	if service != "" && service != "nginx" && service != "haproxy" {
		return UninstallPlan{}, fmt.Errorf("unsupported reload service: %s", req.Service)
	}
	if req.ReloadService && service == "" {
		return UninstallPlan{}, errors.New("service is required when reload is enabled")
	}
	preview := "remove files: " + strings.Join(paths, ", ")
	if req.ReloadService {
		preview += "; systemctl reload " + service
	}
	return UninstallPlan{
		Paths:         paths,
		Service:       service,
		ReloadService: req.ReloadService,
		Preview:       preview,
	}, nil
}

func RunUninstall(ctx context.Context, plan UninstallPlan, emit func(stream, text string)) (int, error) {
	for _, file := range plan.Paths {
		info, err := os.Stat(file)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				emit("stdout", "missing: "+file)
				continue
			}
			emit("stderr", fmt.Sprintf("stat %s: %v", file, err))
			return -1, err
		}
		if info.IsDir() {
			err := fmt.Errorf("refusing to remove directory: %s", file)
			emit("stderr", err.Error())
			return -1, err
		}
		if err := os.Remove(file); err != nil {
			emit("stderr", fmt.Sprintf("remove %s: %v", file, err))
			return -1, err
		}
		emit("stdout", "removed: "+file)
	}
	if plan.ReloadService {
		cmd := exec.CommandContext(ctx, "systemctl", "reload", plan.Service)
		out, err := cmd.CombinedOutput()
		if len(out) > 0 {
			emit("stdout", strings.TrimSpace(string(out)))
		}
		if err != nil {
			emit("stderr", err.Error())
			return -1, err
		}
		emit("stdout", "reloaded: "+plan.Service)
	}
	return 0, nil
}
