package sessions

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/docker"
	"github.com/floatshadow/emacs-playground/internal/docker/dockertest"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestDockerRemoteBox(t *testing.T) {
	ctx := context.Background()
	fake := dockertest.New()
	fake.AddContainer(docker.ContainerSpec{Name: "gw"}, "running")
	b := &DockerBackend{Client: fake, Version: "v1", Isolate: true, Gateway: "gw"}
	spec := Spec{ID: "s1", UserID: "u1", Token: "t", Image: "session:dev", RemoteImage: "box:dev", Volume: "pg-ws-u1"}
	h, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	box, ok := fake.Container(h.RemoteContainerID)
	if !ok || box.Spec.Name != "pg-box-s1" || box.Spec.Network != NetworkName("s1") || box.Spec.Labels[docker.LabelRole] != docker.RoleRemoteBox {
		t.Fatalf("box %+v", box)
	}
	if box.Spec.CapDrop[0] != "ALL" || len(box.Spec.CapAdd) == 0 || box.Spec.Init != true {
		t.Fatalf("box profile %+v", box.Spec)
	}
	sess, _ := fake.Container(h.ContainerID)
	// Alone on the session network, the box answers to "box".
	if sess.Spec.Env["PG_BOX_HOST"] != protocol.BoxHost {
		t.Fatalf("PG_BOX_HOST %q", sess.Spec.Env["PG_BOX_HOST"])
	}
	k, err := protocol.ParseBoxKey([]byte(sess.Spec.Env["PG_BOX_KEY"]))
	if err != nil || protocol.AuthorizedKey(k.Public().(ed25519.PublicKey), "playground-s1") != box.Spec.Env["AUTHORIZED_KEYS"] {
		t.Fatalf("key pair mismatch: %v", err)
	}
	if err := b.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	if c, _ := fake.Container(h.RemoteContainerID); c.State != "running" {
		t.Fatalf("box state %s", c.State)
	}
	hs, _ := b.List(ctx)
	if len(hs) != 1 || hs[0].RemoteContainerID != h.RemoteContainerID {
		t.Fatalf("list %+v", hs)
	}
	// Re-creating the session replaces its box; removing removes it.
	if err := b.Remove(ctx, h, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := fake.Container(h.RemoteContainerID); !c.Removed {
		t.Fatal("box survived Remove")
	}

	// A box that cannot be created leaves the session without one.
	fake.FailCreate = errors.New("no such image")
	h2, err := b.Create(ctx, spec)
	if err != nil || h2.RemoteContainerID != "" || b.BoxErr == nil {
		t.Fatalf("create without box: %+v %v %v", h2, err, b.BoxErr)
	}
	sess, _ = fake.Container(h2.ContainerID)
	if sess.Spec.Env["PG_BOX_HOST"] != "" || sess.Spec.Env["PG_BOX_KEY"] != "" {
		t.Fatalf("env without a box %v", sess.Spec.Env)
	}
}
