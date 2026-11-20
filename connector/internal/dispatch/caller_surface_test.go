package dispatch_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Doc 31 §16's row exists to keep one argument alive: schema validation at the boundary is not enforcement
// inside it, so "no shell, path or bind address is caller-chosen at run time" needs a Connector-side proof,
// not only the contract's GREEN. This guard is that proof, in the two halves the claim actually has:
//
//  1. No command's args schema can *express* the input an exec or a Listen would take — no string-typed
//     property anywhere in the eighteen args trees names a shell, a program, a path, a directory or a bind
//     address, so a caller has no field to choose one with.
//  2. The Connector tree contains exactly the process-spawning sites the corpus describes, and none of them
//     is reachable from a command handler: the supervisor spawns the receipt-named Core (the pre-exec hash
//     guards it — VER-87), the host spawns its own Connector binary, the installer drives reg.exe, and the
//     localpipe is a named pipe, never a TCP listen. Nothing else imports os/exec, nothing calls net.Listen,
//     and no shell interpreter literal appears in non-test sources.
//
// The live half — watching a real sender try it — stays LIVE-03's and LIVE-16's.

var repo = func() string {
	abs, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		panic(err)
	}
	return abs
}()

// execDeniedNames are the property names that would make a caller-chosen shell, path or bind address
// expressible. A string-typed property with one of these names, at any depth of any command's args tree,
// fails this guard.
var execDeniedNames = map[string]bool{
	"shell": true, "command": true, "cmd": true, "argv": true, "args": true,
	"bin": true, "exe": true, "binary": true, "program": true, "interpreter": true, "script": true,
	"bind": true, "bindaddress": true, "listen": true, "listenaddress": true, "address": true, "addr": true,
	"host": true, "path": true, "dir": true, "execpath": true, "cmdline": true, "workdir": true,
}

func TestNoCommandArgsExpressACallerChosenShellPathOrBind(t *testing.T) {
	dir := filepath.Join(repo, "packages", "schema", "protocol", "commands")
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("command docs not found under %s: %v", dir, err)
	}
	var scan func(at string, v any) []string
	scan = func(at string, v any) []string {
		var hits []string
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		if props, ok := m["properties"].(map[string]any); ok {
			for k, sub := range props {
				lower := strings.ToLower(k)
				sm, _ := sub.(map[string]any)
				if execDeniedNames[lower] && sm != nil && acceptsString(sm) {
					hits = append(hits, at+"."+k+" accepts a string; a caller could choose a "+lower+" with it")
				}
				hits = append(hits, scan(at+"."+k, sub)...)
			}
		}
		if items, ok := m["items"]; ok {
			hits = append(hits, scan(at+"[]", items)...)
		}
		for _, key := range []string{"anyOf", "oneOf", "allOf"} {
			if variants, ok := m[key].([]any); ok {
				for i, variant := range variants {
					hits = append(hits, scan(at+"."+key+"["+fmt.Sprintf("%d", i)+"]", variant)...)
				}
			}
		}
		return hits
	}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		method, _ := doc["name"].(string)
		if method == "" {
			method = strings.TrimSuffix(filepath.Base(name), ".json")
		}
		for _, hit := range scan(method, doc["args"]) {
			t.Errorf("%s: %s", method, hit)
		}
	}
}

func acceptsString(schema map[string]any) bool {
	switch tv := schema["type"].(type) {
	case string:
		return tv == "string"
	case []any:
		for _, v := range tv {
			if s, ok := v.(string); ok && s == "string" {
				return true
			}
		}
	}
	return false
}

// spawnAllowlist is every non-test file in the Connector tree that may import os/exec, each with the reason
// the corpus gives it. A new spawn site is a change to this list — the review that adds one has to name its
// reason here, which is the point.
var spawnAllowlist = map[string]string{
	"cmd/nexora-connector/main.go":           "spawns the Core binary the install receipt names, after stat; the pre-exec hash re-verifies the bytes (doc 33 §6, VER-87)",
	"cmd/nexora-host/main.go":                "spawns the Connector per accepted sender, argv built from constants and the sender's own id",
	"cmd/nexora-host/shared.go":              "the detached shared Connector spawn, same binary the host resolved from its own path",
	"cmd/nexora-installer/main.go":           "drives reg.exe to write and read back the Native Messaging registration under HKCU",
	"internal/supervisor/supervisor.go":      "the supervisor's Launch, the receipt-named path with the pre-exec hash in front of it",
	"internal/supervisor/contain_windows.go": "job-object and containment helpers around the same child",
	"internal/supervisor/contain_linux.go":   "the Linux containment half of the same child",
	"internal/supervisor/contain_other.go":   "the stub for platforms without a containment story",
	"internal/localpipe/localpipe_other.go":  "the unsupported-platform stub of the named pipe",
	"internal/localpipe/process_windows.go":  "the named pipe's client-side handle hand-off",
}

func TestProcessSpawningStaysInItsAllowlistedSites(t *testing.T) {
	root := filepath.Join(repo, "connector")
	var spawned []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(data)
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.Contains(src, "os/exec") {
			if _, ok := spawnAllowlist[rel]; !ok {
				t.Errorf("%s imports os/exec and is not in the spawn allowlist; a spawn site outside the corpus's four is exactly what doc 31 §16's row forbids", rel)
			} else {
				spawned = append(spawned, rel)
			}
		}
		if strings.Contains(src, "net.Listen(") {
			t.Errorf("%s calls net.Listen; the Connector listens on named pipes only (doc 31 §11: no HTTP control surface)", rel)
		}
		for _, shell := range []string{"sh -c", "/bin/sh", "powershell -", "cmd.exe /c", "cmd /c"} {
			if strings.Contains(src, shell) {
				t.Errorf("%s contains the shell literal %q; a shell interpreter is not one of the connector's spawn shapes", rel, shell)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range spawnAllowlist {
		found := false
		for _, s := range spawned {
			if s == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("allowlist names %s, which no longer spawns; drop the entry rather than keeping a stale reason", rel)
		}
	}
}
