package actions

import "testing"

func TestBuildUninstallRequiresConfirm(t *testing.T) {
	_, err := BuildUninstall(UninstallRequest{
		Paths: []string{"/etc/nginx/ssl/example.com/fullchain.pem"},
	})
	if err == nil {
		t.Fatal("expected confirm error")
	}
}

func TestBuildUninstallPlan(t *testing.T) {
	plan, err := BuildUninstall(UninstallRequest{
		Paths:         []string{"/etc/nginx/ssl/example.com/fullchain.pem\n/etc/nginx/ssl/example.com/key.pem"},
		Service:       "nginx",
		ReloadService: true,
		Confirm:       "DELETE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) != 2 {
		t.Fatalf("unexpected paths: %#v", plan.Paths)
	}
	if plan.Service != "nginx" || !plan.ReloadService {
		t.Fatalf("bad plan: %#v", plan)
	}
}
