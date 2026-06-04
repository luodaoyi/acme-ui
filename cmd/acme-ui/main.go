package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/acme-ui/acme-ui/internal/acme"
	"github.com/acme-ui/acme-ui/internal/auth"
	"github.com/acme-ui/acme-ui/internal/jobs"
	"github.com/acme-ui/acme-ui/internal/server"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	cfg := parseFlags()
	if cfg.showVersion {
		fmt.Printf("acme-ui %s (%s)\n", version, commit)
		return
	}

	if runtime.GOOS != "linux" && os.Getenv("ACME_UI_ALLOW_NON_LINUX") != "1" {
		fmt.Fprintln(os.Stderr, "acme-ui is Linux-only. Set ACME_UI_ALLOW_NON_LINUX=1 for local development only.")
		os.Exit(1)
	}

	masterKey := cfg.masterKey
	if masterKey == "" {
		var err error
		masterKey, err = auth.GenerateToken(32)
		if err != nil {
			log.Fatalf("generate masterKey: %v", err)
		}
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.bind, cfg.port))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	store := jobs.NewStore()
	locator := acme.NewLocator(cfg.acmePath, cfg.acmeHome)
	web := server.New(server.Config{
		Bind:      cfg.bind,
		Port:      actualPort,
		Version:   version,
		Commit:    commit,
		MasterKey: masterKey,
		Locator:   locator,
		Jobs:      store,
	})
	httpServer := &http.Server{
		Handler:           web.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	printStartup(cfg.bind, actualPort, masterKey, locator.Resolve())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("\nshutting down acme-ui...")
	store.CancelAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

type cliConfig struct {
	bind        string
	port        int
	acmePath    string
	acmeHome    string
	masterKey   string
	showVersion bool
}

func parseFlags() cliConfig {
	var cfg cliConfig
	flag.StringVar(&cfg.bind, "bind", "0.0.0.0", "HTTP bind address")
	flag.IntVar(&cfg.port, "port", 0, "HTTP port, 0 means random")
	flag.StringVar(&cfg.acmePath, "acme", "", "path to acme.sh")
	flag.StringVar(&cfg.acmeHome, "home", "", "acme.sh home directory")
	flag.StringVar(&cfg.masterKey, "master-key", "", "fixed masterKey for automation; defaults to random")
	flag.BoolVar(&cfg.showVersion, "version", false, "print version and exit")
	flag.Parse()
	return cfg
}

func printStartup(bind string, port int, masterKey string, resolved acme.Resolved) {
	fmt.Println("acme-ui is running")
	fmt.Println()
	fmt.Printf("Listen:    %s:%d\n", bind, port)
	for _, url := range publicURLs(bind, port) {
		fmt.Printf("Open:      %s\n", url)
	}
	fmt.Printf("MasterKey: %s\n", masterKey)
	if resolved.Home != "" {
		fmt.Printf("Home:      %s\n", resolved.Home)
	}
	if resolved.Found {
		fmt.Printf("acme.sh:   %s\n", resolved.Path)
	} else if resolved.Error != "" {
		fmt.Printf("acme.sh:   not found (%s)\n", resolved.Error)
	} else {
		fmt.Println("acme.sh:   not found")
	}
	fmt.Println("Mode:      foreground, press Ctrl+C to exit")
	fmt.Println()
}

func publicURLs(bind string, port int) []string {
	if bind != "0.0.0.0" && bind != "::" {
		return []string{fmt.Sprintf("http://%s:%d", hostForURL(bind), port)}
	}
	var urls []string
	for _, ip := range publicIPs() {
		urls = append(urls, fmt.Sprintf("http://%s:%d", hostForURL(ip), port))
	}
	ips := localIPs()
	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%d", hostForURL(ip), port))
	}
	if len(urls) == 0 {
		urls = append(urls, fmt.Sprintf("http://<server-ip>:%d", port))
	}
	return urls
}

func publicIPs() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	endpoints := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
	}
	seen := make(map[string]bool)
	var ips []string
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 128))
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		ip := strings.TrimSpace(string(body))
		addr, err := netip.ParseAddr(ip)
		if err != nil || !isPublicAddr(addr) {
			continue
		}
		normalized := addr.String()
		if !seen[normalized] {
			seen[normalized] = true
			ips = append(ips, normalized)
		}
	}
	return ips
}

func isPublicAddr(addr netip.Addr) bool {
	return addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!addr.IsLinkLocalMulticast() &&
		!addr.IsMulticast() &&
		!addr.IsUnspecified()
}

func hostForURL(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func localIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				ips = append(ips, v4.String())
			}
		}
	}
	return ips
}
