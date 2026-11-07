package apply

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/contractgen"
)

const driveXrayBin = "NEXORA_XRAY_BIN"
const driveSecret = "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01" // nexora-secret-allow: fixture id invented for this test

func pinnedXray(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(driveXrayBin)
	if bin == "" {
		t.Skipf("%s is not set", driveXrayBin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "Xray 24.11.11 ") {
		t.Fatalf("expected pinned Xray 24.11.11: %v: %s", err, out)
	}
	return bin
}

func xrayTest(t *testing.T, bin, file string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "run", "-test", "-c", file).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("Xray config test timed out: %s", out)
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatalf("running %s: %v", bin, err)
	}
	return 0, string(out)
}

func TestTheMaterializedGoldensAreConfigsThePinnedReleaseAccepts(t *testing.T) {
	bin := pinnedXray(t)
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}
	dir := filepath.Join(root, "connector", "testdata", "config")
	for _, name := range []string{
		"shape-a-vision-tcp-tls.golden.json",
		"shape-b-reality-xhttp.golden.json",
		"multi-server-ordered.golden.json",
		"two-plane-core-rules.golden.json",
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			input := compile.Input{
				Settings: driveExample[compile.Settings](t, root, "settings.default.json"),
				Failure:  driveExample[compile.Failure](t, root, "failure-policy.default.json"),
			}
			vision := driveExample[compile.Server](t, root, "server.vision-tcp-tls.json")
			reality := driveExample[compile.Server](t, root, "server.reality-xhttp.json")
			profile := driveExample[compile.Profile](t, root, "profile.fixed.json")
			input.Servers = []compile.Server{vision}
			if name == "shape-b-reality-xhttp.golden.json" {
				input.Servers = []compile.Server{reality}
			}
			if name == "multi-server-ordered.golden.json" {
				profile = driveExample[compile.Profile](t, root, "profile.ordered.json")
				input.Servers = []compile.Server{reality, vision}
			}
			profile.ServerIDs = nil
			for _, server := range input.Servers {
				profile.ServerIDs = append(profile.ServerIDs, server.ID)
			}
			profile.DefaultServerID = profile.ServerIDs[0]
			input.Profiles = []compile.Profile{profile}
			if name == "two-plane-core-rules.golden.json" {
				policy := driveExample[compile.RoutingPolicy](t, root, "routing.two-plane.json")
				input.Routing = &policy
			}
			compiled, err := compile.Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != string(compiled.Bytes) {
				t.Fatal("drive input differs from the pinned golden")
			}
			out, err := Write(t.TempDir(), compiled.Document, fakeStore{
				"keystore://server/3f2a1c9e/user-id": driveSecret,
				"keystore://server/8b1d4e70/user-id": driveSecret,
			})
			if err != nil {
				t.Fatalf("%s: materializing golden: %v", name, err)
			}
			code, text := xrayTest(t, bin, out.Path)
			if code != 0 {
				t.Errorf("Xray refused %s (exit %d): %s", name, code, text)
			}
		})
	}
}

func driveExample[T any](t *testing.T, root, name string) T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	value, err := compile.Decode[T](raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestTheDriveProbeRefusesABrokenConfig(t *testing.T) {
	bin := pinnedXray(t)
	file := filepath.Join(t.TempDir(), "broken.json")
	broken := `{"inbounds":[{"listen":"127.0.0.1","port":"not-a-port","protocol":"socks"}],"outbounds":[]}`
	if err := os.WriteFile(file, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _ := xrayTest(t, bin, file)
	if code == 0 {
		t.Error("Xray accepted a non-numeric port")
	}
}
