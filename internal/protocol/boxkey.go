package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
)

// The remote box key (deployment.md §6): an ed25519 pair per session. The
// private half travels as PG_BOX_KEY in the unencrypted OpenSSH format
// ssh-keygen writes (PROTOCOL.key); the public half is an authorized_keys
// line. The gateway's docker backend generates it, sessiond writes it.

// GenerateBoxKey returns a new pair: the private key in OpenSSH format and
// the authorized_keys line.
func GenerateBoxKey(comment string) (privateKey []byte, authorizedKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	b, err := MarshalBoxKey(priv, comment)
	if err != nil {
		return nil, "", err
	}
	return b, AuthorizedKey(pub, comment), nil
}

// AuthorizedKey renders "ssh-ed25519 <base64> <comment>".
func AuthorizedKey(pub ed25519.PublicKey, comment string) string {
	s := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(pubBlob(pub))
	if comment != "" {
		s += " " + comment
	}
	return s
}

const keyType = "ssh-ed25519"

func sshString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

func pubBlob(pub ed25519.PublicKey) []byte {
	return append(sshString([]byte(keyType)), sshString(pub)...)
}

// openSSHMagic starts the "openssh-key-v1" container.
const openSSHMagic = "openssh-key-v1\x00"

// MarshalBoxKey encodes an unencrypted OpenSSH private key (the format
// ssh-keygen writes, PROTOCOL.key).
func MarshalBoxKey(priv ed25519.PrivateKey, comment string) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("box key: bad ed25519 key")
	}
	pub := priv.Public().(ed25519.PublicKey)
	var check [4]byte
	if _, err := rand.Read(check[:]); err != nil {
		return nil, err
	}
	var inner []byte
	inner = append(inner, check[:]...)
	inner = append(inner, check[:]...)
	inner = append(inner, sshString([]byte(keyType))...)
	inner = append(inner, sshString(pub)...)
	inner = append(inner, sshString(priv)...)
	inner = append(inner, sshString([]byte(comment))...)
	for i := byte(1); len(inner)%8 != 0; i++ {
		inner = append(inner, i)
	}
	var out []byte
	out = append(out, openSSHMagic...)
	out = append(out, sshString([]byte("none"))...) // cipher
	out = append(out, sshString([]byte("none"))...) // kdf
	out = append(out, sshString(nil)...)            // kdf options
	out = binary.BigEndian.AppendUint32(out, 1)     // one key
	out = append(out, sshString(pubBlob(pub))...)
	out = append(out, sshString(inner)...)
	return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: out}), nil
}

// ParseBoxKey decodes an unencrypted OpenSSH ed25519 private key.
func ParseBoxKey(data []byte) (ed25519.PrivateKey, error) {
	blk, _ := pem.Decode(data)
	if blk == nil || blk.Type != "OPENSSH PRIVATE KEY" {
		return nil, errors.New("not an OpenSSH private key")
	}
	if !bytes.HasPrefix(blk.Bytes, []byte(openSSHMagic)) {
		return nil, errors.New("not an openssh-key-v1 key")
	}
	r := &reader{b: blk.Bytes[len(openSSHMagic):]}
	cipher, kdf := r.str(), r.str()
	r.str() // kdf options
	if string(cipher) != "none" || string(kdf) != "none" {
		return nil, errors.New("encrypted keys are not supported")
	}
	if r.u32() != 1 {
		return nil, errors.New("want exactly one key")
	}
	r.str() // public blob
	inner := &reader{b: r.str()}
	if inner.u32() != inner.u32() {
		return nil, errors.New("corrupt key (check bytes differ)")
	}
	if string(inner.str()) != keyType {
		return nil, errors.New("not an ed25519 key")
	}
	inner.str() // public key
	priv := inner.str()
	if r.bad || inner.bad || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("corrupt key")
	}
	return ed25519.PrivateKey(bytes.Clone(priv)), nil
}

// reader consumes SSH wire encoding; a short buffer yields zero values and
// sets bad, checked once at the end.
type reader struct {
	b   []byte
	bad bool
}

func (r *reader) u32() uint32 {
	if len(r.b) < 4 {
		r.b, r.bad = nil, true
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

func (r *reader) str() []byte {
	n := r.u32()
	if uint64(len(r.b)) < uint64(n) {
		r.b, r.bad = nil, true
		return nil
	}
	s := r.b[:n]
	r.b = r.b[n:]
	return s
}
