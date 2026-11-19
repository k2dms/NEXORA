package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// adoptionOwnerEnv marks a test binary that is acting as the shared Connector owner in
// TestAdoptionAcrossPortRecreation. Reaching this helper without the marker set is a mistake, and skipping is how
// a helper stays out of the run's report.
const adoptionOwnerEnv = "NEXORA_ADOPTION_OWNER"

// TestAdoptionSharedOwner is not a test; it is the child. It runs the Connector in shared mode against the temp
// tree the parent test prepared, holds the instance mutex, and serves the one sender the parent will adopt. It
// leaves by os.Exit so the Go test framework never prints onto the stdout the parent's pipe is made of.
func TestAdoptionSharedOwner(t *testing.T) {
	if os.Getenv(adoptionOwnerEnv) != "1" {
		t.Skip("helper process for the adoption test")
	}
	stateDir := os.Getenv("NEXORA_ADOPTION_STATE_DIR")
	configDir := os.Getenv("NEXORA_ADOPTION_CONFIG_DIR")
	keystoreDir := os.Getenv("NEXORA_ADOPTION_KEYSTORE_DIR")
	coreDir := os.Getenv("NEXORA_ADOPTION_CORE_DIR")
	instanceRoot := os.Getenv("NEXORA_ADOPTION_INSTANCE_ROOT")
	hostExe := os.Getenv("NEXORA_ADOPTION_HOST_EXE")
	version := os.Getenv("NEXORA_ADOPTION_VERSION")
	senderIDsRaw := os.Getenv("NEXORA_ADOPTION_SENDER_IDS")
	var senderIDs []string
	if err := json.Unmarshal([]byte(senderIDsRaw), &senderIDs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg := config{
		shared:       true,
		instanceRoot: instanceRoot,
		hostExe:      hostExe,
		version:      version,
		senderIDs:    senderIDs,
		stateDir:     stateDir,
		configDir:    configDir,
		keystoreDir:  keystoreDir,
		coreDir:      coreDir,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runShared(ctx, cfg, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
