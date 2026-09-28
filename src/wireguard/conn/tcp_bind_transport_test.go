package conn

import "testing"

// Agents open the bind before Master's transport config arrives; a later push
// must reach the open bind or the MC login secret never matches the server.
func TestTransportPushReachesOpenBind(t *testing.T) {
	t.Setenv("LASITAN_CONF_DIR", t.TempDir())
	SetTransportConfigJSON(nil)
	t.Cleanup(func() { SetTransportConfigJSON(nil) })

	b := NewTCPBind().(*TCPBind)
	if _, _, err := b.Open(0); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := b.getMCConfig().pluginSecret; got != defaultLoginPluginSecret {
		t.Fatalf("before push: secret %q", got)
	}

	SetTransportConfigJSON([]byte(`{"mc":{"enabled":true,"deepCamouflage":true,"loginPluginSecret":"from-master"}}`))
	if got := b.getMCConfig().pluginSecret; got != "from-master" {
		t.Fatalf("after push: secret %q", got)
	}

	SetTransportConfigJSON(nil)
	if got := b.getMCConfig().pluginSecret; got != defaultLoginPluginSecret {
		t.Fatalf("after clear: secret %q", got)
	}
}
