package acme

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acme-ui/acme-ui/internal/security"
)

type Locator struct {
	explicitPath string
	explicitHome string
}

type Resolved struct {
	Path  string `json:"path"`
	Home  string `json:"home"`
	Found bool   `json:"found"`
	Error string `json:"error,omitempty"`
}

func NewLocator(explicitPath, explicitHome string) *Locator {
	return &Locator{
		explicitPath: strings.TrimSpace(explicitPath),
		explicitHome: strings.TrimSpace(explicitHome),
	}
}

func (l *Locator) Resolve() Resolved {
	home := l.explicitHome
	if home == "" {
		home = defaultAcmeHome()
	}
	if l.explicitPath != "" {
		return checkPath(l.explicitPath, home)
	}
	if envPath := strings.TrimSpace(os.Getenv("ACME_SH")); envPath != "" {
		resolved := checkPath(envPath, home)
		if resolved.Found {
			return resolved
		}
	}
	if found, err := exec.LookPath("acme.sh"); err == nil {
		return checkPath(found, home)
	}
	if home != "" {
		return checkPath(path.Join(home, "acme.sh"), home)
	}
	return Resolved{Home: home, Error: "unable to resolve HOME"}
}

func (l *Locator) WithHome(args []string) []string {
	if l.explicitHome == "" {
		return args
	}
	return append([]string{"--home", l.explicitHome}, args...)
}

func defaultAcmeHome() string {
	if home := strings.TrimSpace(os.Getenv("LE_WORKING_DIR")); home != "" {
		return home
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return path.Join(home, ".acme.sh")
	}
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		return path.Join(current.HomeDir, ".acme.sh")
	}
	return ""
}

func checkPath(candidate, home string) Resolved {
	if candidate == "" {
		return Resolved{Home: home, Error: "empty path"}
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return Resolved{Path: candidate, Home: home, Error: err.Error()}
	}
	if info.IsDir() {
		return Resolved{Path: candidate, Home: home, Error: "path is directory"}
	}
	return Resolved{Path: candidate, Home: home, Found: true}
}

type CommandSpec struct {
	Kind    string            `json:"kind"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"-"`
	Secrets []string          `json:"-"`
	Preview string            `json:"preview"`
}

type IssueRequest struct {
	Domains     []string `json:"domains"`
	CFMode      string   `json:"cfMode"`
	CFToken     string   `json:"cfToken"`
	CFAccountID string   `json:"cfAccountId"`
	CFZoneID    string   `json:"cfZoneId"`
	CFKey       string   `json:"cfKey"`
	CFEmail     string   `json:"cfEmail"`
	CA          string   `json:"ca"`
	KeyLength   string   `json:"keyLength"`
	DNSSleep    int      `json:"dnsSleep"`
	Force       bool     `json:"force"`
	Staging     bool     `json:"staging"`
	Debug       bool     `json:"debug"`
}

type InstallRequest struct {
	Domain        string `json:"domain"`
	ECC           bool   `json:"ecc"`
	Server        string `json:"server"`
	KeyFile       string `json:"keyFile"`
	FullchainFile string `json:"fullchainFile"`
	WorkDir       string `json:"workDir"`
	PEMFile       string `json:"pemFile"`
	ReloadService bool   `json:"reloadService"`
}

type DomainRequest struct {
	Domain string `json:"domain"`
	ECC    bool   `json:"ecc"`
	Force  bool   `json:"force"`
}

func BuildIssue(req IssueRequest) (CommandSpec, error) {
	domains, err := normalizeDomains(req.Domains)
	if err != nil {
		return CommandSpec{}, err
	}
	keyLength := strings.TrimSpace(req.KeyLength)
	if keyLength == "" {
		keyLength = "ec-256"
	}
	if !validKeyLength(keyLength) {
		return CommandSpec{}, fmt.Errorf("unsupported key length: %s", keyLength)
	}

	args := []string{"--issue", "--dns", "dns_cf"}
	for _, domain := range domains {
		args = append(args, "-d", domain)
	}
	args = append(args, "--keylength", keyLength)

	if ca := strings.TrimSpace(req.CA); ca != "" {
		if !validCA(ca) {
			return CommandSpec{}, fmt.Errorf("unsupported CA: %s", ca)
		}
		args = append(args, "--server", ca)
	}
	if req.DNSSleep > 0 {
		if req.DNSSleep > 3600 {
			return CommandSpec{}, errors.New("dnsSleep must be <= 3600")
		}
		args = append(args, "--dnssleep", fmt.Sprintf("%d", req.DNSSleep))
	}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Staging {
		args = append(args, "--staging")
	}
	if req.Debug {
		args = append(args, "--debug")
	}

	env, secrets, err := cloudflareEnv(req)
	if err != nil {
		return CommandSpec{}, err
	}
	return CommandSpec{
		Kind:    "issue",
		Args:    args,
		Env:     env,
		Secrets: secrets,
		Preview: Preview("acme.sh", args, env),
	}, nil
}

func BuildInstall(req InstallRequest) (CommandSpec, error) {
	domain, err := NormalizeDomain(req.Domain)
	if err != nil {
		return CommandSpec{}, err
	}
	args := []string{"--install-cert", "-d", domain}
	if req.ECC {
		args = append(args, "--ecc")
	}
	server := strings.ToLower(strings.TrimSpace(req.Server))
	switch server {
	case "nginx":
		keyFile, err := NormalizeAbsPath(req.KeyFile)
		if err != nil {
			return CommandSpec{}, fmt.Errorf("keyFile: %w", err)
		}
		fullchainFile, err := NormalizeAbsPath(req.FullchainFile)
		if err != nil {
			return CommandSpec{}, fmt.Errorf("fullchainFile: %w", err)
		}
		args = append(args, "--key-file", keyFile, "--fullchain-file", fullchainFile)
		if req.ReloadService {
			args = append(args, "--reloadcmd", "systemctl reload nginx")
		}
	case "haproxy":
		workDir, err := NormalizeAbsPath(req.WorkDir)
		if err != nil {
			return CommandSpec{}, fmt.Errorf("workDir: %w", err)
		}
		pemFile, err := NormalizeAbsPath(req.PEMFile)
		if err != nil {
			return CommandSpec{}, fmt.Errorf("pemFile: %w", err)
		}
		keyFile := path.Join(workDir, domain, "key.pem")
		fullchainFile := path.Join(workDir, domain, "fullchain.pem")
		reload := fmt.Sprintf("cat %s %s > %s", fullchainFile, keyFile, pemFile)
		if req.ReloadService {
			reload += " && systemctl reload haproxy"
		}
		args = append(args,
			"--key-file", keyFile,
			"--fullchain-file", fullchainFile,
			"--reloadcmd", reload,
		)
	default:
		return CommandSpec{}, fmt.Errorf("unsupported install server: %s", req.Server)
	}
	return CommandSpec{
		Kind:    "install",
		Args:    args,
		Preview: Preview("acme.sh", args, nil),
	}, nil
}

func BuildRenew(req DomainRequest) (CommandSpec, error) {
	return buildDomainCommand("renew", "--renew", req)
}

func BuildRevoke(req DomainRequest) (CommandSpec, error) {
	return buildDomainCommand("revoke", "--revoke", req)
}

func BuildRemove(req DomainRequest) (CommandSpec, error) {
	req.Force = false
	return buildDomainCommand("remove", "--remove", req)
}

func BuildInfo(req DomainRequest) (CommandSpec, error) {
	req.Force = false
	return buildDomainCommand("info", "--info", req)
}

func BuildList() CommandSpec {
	args := []string{"--list"}
	return CommandSpec{Kind: "list", Args: args, Preview: Preview("acme.sh", args, nil)}
}

func buildDomainCommand(kind, flag string, req DomainRequest) (CommandSpec, error) {
	domain, err := NormalizeDomain(req.Domain)
	if err != nil {
		return CommandSpec{}, err
	}
	args := []string{flag, "-d", domain}
	if req.ECC {
		args = append(args, "--ecc")
	}
	if req.Force {
		args = append(args, "--force")
	}
	return CommandSpec{Kind: kind, Args: args, Preview: Preview("acme.sh", args, nil)}, nil
}

func Run(ctx context.Context, resolved Resolved, args []string, env map[string]string, secrets []string, emit func(stream, text string)) (int, error) {
	if !resolved.Found {
		return -1, errors.New("acme.sh not found")
	}
	cmd := exec.CommandContext(ctx, resolved.Path, args...)
	cmd.Env = mergedEnv(env)
	redactor := security.NewRedactor(secrets)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanPipe(&wg, stdout, "stdout", redactor, emit)
	go scanPipe(&wg, stderr, "stderr", redactor, emit)
	wg.Wait()

	err = cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return -1, err
}

func RunSync(ctx context.Context, resolved Resolved, args []string, env map[string]string, secrets []string) (string, int, error) {
	if !resolved.Found {
		return "", -1, errors.New("acme.sh not found")
	}
	cmd := exec.CommandContext(ctx, resolved.Path, args...)
	cmd.Env = mergedEnv(env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		if out != "" {
			out += "\n"
		}
		out += stderr.String()
	}
	redactor := security.NewRedactor(secrets)
	out = redactor.Redact(out)
	if err == nil {
		return out, 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, exitErr.ExitCode(), err
	}
	return out, -1, err
}

func scanPipe(wg *sync.WaitGroup, reader io.Reader, stream string, redactor security.Redactor, emit func(stream, text string)) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		emit(stream, redactor.Redact(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		emit("stderr", redactor.Redact(err.Error()))
	}
}

func mergedEnv(env map[string]string) []string {
	merged := os.Environ()
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		merged = append(merged, key+"="+env[key])
	}
	return merged
}

func ToolStatus(names ...string) map[string]string {
	status := make(map[string]string, len(names))
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			status[name] = path
		} else {
			status[name] = ""
		}
	}
	return status
}

func Preview(binary string, args []string, env map[string]string) string {
	var parts []string
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"=****")
	}
	parts = append(parts, ShellQuote(binary))
	for _, arg := range args {
		parts = append(parts, ShellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`).MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func cloudflareEnv(req IssueRequest) (map[string]string, []string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.CFMode))
	if mode == "" {
		mode = "account"
	}
	env := make(map[string]string)
	var secrets []string
	switch mode {
	case "account":
		token := strings.TrimSpace(req.CFToken)
		accountID := strings.TrimSpace(req.CFAccountID)
		if token == "" || accountID == "" {
			return nil, nil, errors.New("CF_Token and CF_Account_ID are required")
		}
		env["CF_Token"] = token
		env["CF_Account_ID"] = accountID
		secrets = append(secrets, token)
	case "zone":
		token := strings.TrimSpace(req.CFToken)
		zoneID := strings.TrimSpace(req.CFZoneID)
		if token == "" || zoneID == "" {
			return nil, nil, errors.New("CF_Token and CF_Zone_ID are required")
		}
		env["CF_Token"] = token
		env["CF_Zone_ID"] = zoneID
		secrets = append(secrets, token)
	case "global":
		key := strings.TrimSpace(req.CFKey)
		email := strings.TrimSpace(req.CFEmail)
		if key == "" || email == "" {
			return nil, nil, errors.New("CF_Key and CF_Email are required")
		}
		env["CF_Key"] = key
		env["CF_Email"] = email
		secrets = append(secrets, key)
	default:
		return nil, nil, fmt.Errorf("unsupported Cloudflare mode: %s", req.CFMode)
	}
	return env, secrets, nil
}

func normalizeDomains(input []string) ([]string, error) {
	var domains []string
	seen := make(map[string]bool)
	for _, item := range input {
		for _, raw := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			domain, err := NormalizeDomain(raw)
			if err != nil {
				return nil, err
			}
			if !seen[domain] {
				seen[domain] = true
				domains = append(domains, domain)
			}
		}
	}
	if len(domains) == 0 {
		return nil, errors.New("at least one domain is required")
	}
	return domains, nil
}

func NormalizeDomain(raw string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	if domain == "" {
		return "", errors.New("domain is required")
	}
	if !domainPattern.MatchString(domain) {
		return "", fmt.Errorf("invalid domain: %s", raw)
	}
	return domain, nil
}

func NormalizeAbsPath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("path is required")
	}
	if !strings.HasPrefix(value, "/") {
		return "", errors.New("path must be absolute")
	}
	if unsafePathPattern.MatchString(value) {
		return "", errors.New("path contains unsupported shell-sensitive characters")
	}
	cleaned := path.Clean(value)
	if cleaned == "/" {
		return "", errors.New("path must not be root")
	}
	return cleaned, nil
}

func validKeyLength(value string) bool {
	switch value {
	case "ec-256", "ec-384", "2048", "3072", "4096":
		return true
	default:
		return false
	}
}

func validCA(value string) bool {
	switch value {
	case "letsencrypt", "zerossl", "buypass", "google":
		return true
	default:
		return strings.HasPrefix(value, "https://")
	}
}

var (
	domainPattern     = regexp.MustCompile(`^(\*\.)?([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	unsafePathPattern = regexp.MustCompile(`[[:space:]'"` + "`" + `$;&|<>\\]`)
)

func VersionOutput(ctx context.Context, resolved Resolved) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return RunSync(ctx, resolved, []string{"--version"}, nil, nil)
}
