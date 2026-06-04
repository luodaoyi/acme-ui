package acme

import "testing"

func TestBuildIssueCloudflareAccount(t *testing.T) {
	spec, err := BuildIssue(IssueRequest{
		Domains:     []string{"example.com", "*.example.com"},
		CFMode:      "account",
		CFToken:     "secret-token",
		CFAccountID: "account-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != "issue" {
		t.Fatalf("unexpected kind: %s", spec.Kind)
	}
	if spec.Env["CF_Token"] != "secret-token" || spec.Env["CF_Account_ID"] != "account-id" {
		t.Fatalf("missing env: %#v", spec.Env)
	}
	if spec.Preview == "" || spec.Preview == "secret-token" {
		t.Fatalf("bad preview: %q", spec.Preview)
	}
}

func TestNormalizeAbsPathRejectsShellChars(t *testing.T) {
	if _, err := NormalizeAbsPath("/etc/haproxy/certs/example.pem;rm"); err == nil {
		t.Fatal("expected unsafe path error")
	}
}

func TestBuildInstallHAProxy(t *testing.T) {
	spec, err := BuildInstall(InstallRequest{
		Domain:        "example.com",
		ECC:           true,
		Server:        "haproxy",
		WorkDir:       "/etc/acme-ui",
		PEMFile:       "/etc/haproxy/certs/example.com.pem",
		ReloadService: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "cat /etc/acme-ui/example.com/fullchain.pem /etc/acme-ui/example.com/key.pem > /etc/haproxy/certs/example.com.pem && systemctl reload haproxy"
	found := false
	for _, arg := range spec.Args {
		if arg == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing reload command in %#v", spec.Args)
	}
}
