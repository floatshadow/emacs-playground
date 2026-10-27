//go:build dockerintegration

// Integration test against a real Docker Engine (DOCKER_HOST or the default
// socket): go test -tags dockerintegration ./internal/docker/
package docker

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestEngineIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e, err := New(os.Getenv("DOCKER_HOST"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Ping(ctx); err != nil {
		t.Skip("no Docker Engine:", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	network := "pg-it-net-" + suffix
	if out, err := exec.Command("docker", "network", "create", "--internal", network).CombinedOutput(); err != nil {
		t.Fatalf("network: %v %s", err, out)
	}
	defer exec.Command("docker", "network", "rm", network).Run()
	if out, err := exec.Command("docker", "pull", "-q", "alpine:3").CombinedOutput(); err != nil {
		t.Fatalf("pull: %v %s", err, out)
	}

	volume := "pg-ws-it-" + suffix
	labels := map[string]string{LabelRole: RoleSession, LabelSession: "it" + suffix, LabelUser: "u-it"}
	if err := e.CreateVolume(ctx, volume, map[string]string{LabelRole: RoleWorkspace, LabelUser: "u-it"}); err != nil {
		t.Fatal(err)
	}
	defer e.RemoveVolume(context.Background(), volume)
	spec := ContainerSpec{
		Name: "pg-session-it" + suffix, Image: "alpine:3", Hostname: "pg-session-it" + suffix,
		// Prove the profile: writable /work, /tmp, /run and ~/.cache;
		// read-only elsewhere; uid 1000; no capabilities.
		Cmd: []string{"sh", "-c", `set -e
id -u > /work/uid
touch /tmp/ok /run/ok /home/learner/.cache/ok
if touch /etc/nope 2>/dev/null; then echo rootfs-writable > /work/fail; fi
grep CapEff /proc/self/status > /work/caps
sleep 300`},
		Env:     map[string]string{"PG_SESSION_ID": "it"},
		Labels:  labels,
		Network: network, Aliases: []string{"pg-session-it" + suffix},
		Volumes:  []VolumeMount{{Volume: volume, Target: "/work"}},
		Runtime:  "runc",
		NanoCPUs: 5e8, Memory: 256 << 20, PidsLimit: 128,
	}
	Harden(&spec)
	// The volume root belongs to root; let uid 1000 write to it for the test.
	prep := exec.Command("docker", "run", "--rm", "-v", volume+":/work", "alpine:3", "chmod", "777", "/work")
	if out, err := prep.CombinedOutput(); err != nil {
		t.Fatalf("prep: %v %s", err, out)
	}
	id, err := e.CreateContainer(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer e.RemoveContainer(context.Background(), id, true)
	if err := e.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	var info Container
	for i := 0; i < 50; i++ {
		info, err = e.InspectContainer(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if info.Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !info.Running || info.Labels[LabelSession] != "it"+suffix {
		t.Fatalf("inspect %+v", info)
	}
	time.Sleep(time.Second)
	read := func(name string) string {
		out, err := exec.Command("docker", "run", "--rm", "-v", volume+":/work:ro", "alpine:3", "cat", "/work/"+name).CombinedOutput()
		if err != nil {
			return "ERR " + string(out)
		}
		return strings.TrimSpace(string(out))
	}
	if uid := read("uid"); uid != "1000" {
		t.Fatalf("uid %q", uid)
	}
	if caps := read("caps"); !strings.HasSuffix(caps, "0000000000000000") {
		t.Fatalf("capabilities %q", caps)
	}
	if fail := read("fail"); !strings.HasPrefix(fail, "ERR") {
		t.Fatalf("root filesystem is writable: %q", fail)
	}
	list, err := e.ListContainers(ctx, map[string]string{LabelSession: "it" + suffix})
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("list %+v %v", list, err)
	}
	vols, err := e.ListVolumes(ctx, map[string]string{LabelRole: RoleWorkspace, LabelUser: "u-it"})
	if err != nil || len(vols) == 0 {
		t.Fatalf("volumes %+v %v", vols, err)
	}
	if err := e.StopContainer(ctx, id, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveContainer(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.InspectContainer(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after remove: %v", err)
	}
	if err := e.RemoveVolume(ctx, volume); err != nil {
		t.Fatal(err)
	}
}

func TestEngineNetworksIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e, err := New(os.Getenv("DOCKER_HOST"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Ping(ctx); err != nil {
		t.Skip("no Docker Engine:", err)
	}
	if out, err := exec.Command("docker", "pull", "-q", "alpine:3").CombinedOutput(); err != nil {
		t.Fatalf("pull: %v %s", err, out)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	name := "pg-it-sessnet-" + suffix
	subnet := netip.MustParsePrefix(fmt.Sprintf("10.231.%d.%d/28", 200+time.Now().Nanosecond()%50, 16*(time.Now().Nanosecond()%16)))
	labels := map[string]string{LabelRole: RoleNetwork, LabelSession: "it" + suffix}
	if _, err := e.CreateNetwork(ctx, NetworkSpec{Name: name, Labels: labels, Internal: true, Subnet: subnet}); err != nil {
		t.Fatal(err)
	}
	defer e.RemoveNetwork(context.Background(), name)
	if _, err := e.CreateNetwork(ctx, NetworkSpec{Name: name, Labels: labels, Internal: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same name: %v", err)
	}
	if _, err := e.CreateNetwork(ctx, NetworkSpec{Name: name + "-b", Internal: true, Subnet: subnet}); !errors.Is(err, ErrSubnetTaken) {
		e.RemoveNetwork(context.Background(), name+"-b")
		t.Fatalf("same subnet: %v", err)
	}
	nets, err := e.ListNetworks(ctx, labels)
	if err != nil || len(nets) != 1 || nets[0].Name != name || !nets[0].Internal || len(nets[0].Subnets) != 1 || nets[0].Subnets[0] != subnet {
		t.Fatalf("list %+v %v", nets, err)
	}
	// A container on the network and a second one (the gateway) joining it.
	run := func(n string, net string) string {
		args := []string{"run", "-d", "--name", n}
		if net != "" {
			args = append(args, "--network", net)
		}
		out, err := exec.Command("docker", append(args, "alpine:3", "sleep", "120")...).CombinedOutput()
		if err != nil {
			t.Fatalf("run %s: %v %s", n, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	sess := "pg-it-sess-" + suffix
	gw := "pg-it-gw-" + suffix
	run(sess, name)
	defer exec.Command("docker", "rm", "-f", sess).Run()
	gwID := run(gw, "")
	defer exec.Command("docker", "rm", "-f", gw).Run()
	// By id prefix, as the gateway names itself (its hostname); twice.
	for i := 0; i < 2; i++ {
		if err := e.ConnectNetwork(ctx, name, gwID[:12], []string{"gateway"}); err != nil {
			t.Fatalf("connect %d: %v", i, err)
		}
	}
	out, err := exec.Command("docker", "exec", sess, "ping", "-c1", "-W2", "gateway").CombinedOutput()
	if err != nil {
		t.Fatalf("the session does not reach the gateway by its alias: %v %s", err, out)
	}
	if err := e.RemoveNetwork(ctx, name); err == nil {
		t.Fatal("removed a network with members")
	}
	for i := 0; i < 2; i++ {
		if err := e.DisconnectNetwork(ctx, name, gwID[:12]); err != nil {
			t.Fatalf("disconnect %d: %v", i, err)
		}
	}
	if err := exec.Command("docker", "rm", "-f", sess).Run(); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveNetwork(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveNetwork(ctx, name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
	if err := e.DisconnectNetwork(ctx, name, gwID[:12]); err != nil {
		t.Fatalf("disconnect from a missing network: %v", err)
	}
}
