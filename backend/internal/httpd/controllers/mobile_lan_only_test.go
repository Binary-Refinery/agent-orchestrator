package controllers

import (
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/mobilebridge"
)

func lanOnlyBridge(t *testing.T, tunnel *fakeTunnel) *BridgeService {
	t.Helper()
	return &BridgeService{
		LAN:         &fakeLAN{},
		ConfigPath:  filepath.Join(t.TempDir(), "mobile.json"),
		DefaultPort: 3011,
		Tunnel:      tunnel,
	}
}

func loadLANOnlyState(t *testing.T, path string) mobilebridge.State {
	t.Helper()
	state, err := mobilebridge.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestLANOnlyEnableDoesNotStartPublicTunnel(t *testing.T) {
	tunnel := &fakeTunnel{}
	bridge := lanOnlyBridge(t, tunnel)
	status, err := bridge.EnableLANOnly()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.Password == "" || !bridge.LAN.Running() {
		t.Fatalf("LAN-only listener did not start: %+v", status)
	}
	if tunnel.startedOn != 0 || !loadLANOnlyState(t, bridge.ConfigPath).NoPublicTunnel {
		t.Fatal("LAN-only enable started a public tunnel or failed to persist the private mode")
	}
}

func TestLANOnlyRestoreDoesNotStartPublicTunnel(t *testing.T) {
	bridge := lanOnlyBridge(t, &fakeTunnel{})
	if _, err := bridge.EnableLANOnly(); err != nil {
		t.Fatal(err)
	}
	state := loadLANOnlyState(t, bridge.ConfigPath)
	tunnel := &fakeTunnel{}
	restarted := &BridgeService{LAN: &fakeLAN{}, ConfigPath: bridge.ConfigPath, DefaultPort: 3011, Tunnel: tunnel}
	if err := restarted.RestoreOnBoot(state); err != nil {
		t.Fatal(err)
	}
	if !restarted.LAN.Running() || tunnel.startedOn != 0 {
		t.Fatal("restored LAN-only listener started a public tunnel or failed to bind")
	}
}

func TestLANOnlyTransitionStopsPublicTunnelWithoutRotatingPassword(t *testing.T) {
	tunnel := &fakeTunnel{}
	bridge := lanOnlyBridge(t, tunnel)
	before, err := bridge.Enable()
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.startedOn != 3011 {
		t.Fatal("Connect Mobile setup did not start the tunnel")
	}
	after, err := bridge.EnableLANOnly()
	if err != nil {
		t.Fatal(err)
	}
	if after.Password != before.Password || !bridge.LAN.Running() || tunnel.stops != 1 {
		t.Fatal("LAN-only transition changed the password, stopped the listener, or left the tunnel running")
	}
	if !loadLANOnlyState(t, bridge.ConfigPath).NoPublicTunnel {
		t.Fatal("LAN-only transition was not persisted")
	}
}

func TestLANOnlyRegenerateKeepsPublicTunnelOff(t *testing.T) {
	tunnel := &fakeTunnel{}
	bridge := lanOnlyBridge(t, tunnel)
	if _, err := bridge.EnableLANOnly(); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Regenerate(); err != nil {
		t.Fatal(err)
	}
	if tunnel.startedOn != 0 || !loadLANOnlyState(t, bridge.ConfigPath).NoPublicTunnel {
		t.Fatal("password rotation reopened the public tunnel")
	}
}

func TestLANOnlyStartRemoteAccessEnablesPublicTunnel(t *testing.T) {
	tunnel := &fakeTunnel{}
	bridge := lanOnlyBridge(t, tunnel)
	before, err := bridge.EnableLANOnly()
	if err != nil {
		t.Fatal(err)
	}
	after, err := bridge.StartRemoteAccess()
	if err != nil {
		t.Fatal(err)
	}
	if after.Password != before.Password || tunnel.startedOn != 3011 || loadLANOnlyState(t, bridge.ConfigPath).NoPublicTunnel {
		t.Fatal("explicit remote access did not persist tunnel mode and start the connector without rotating the password")
	}
}
