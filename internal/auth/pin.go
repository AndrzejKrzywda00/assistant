package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type pinFile struct {
	Version int    `json:"version"`
	Name    string `json:"name,omitempty"`
	Salt    string `json:"salt"`
	Hash    string `json:"hash"`
}

type Manager struct{ path string }

func New(taskPath string) *Manager { return &Manager{path: taskPath + ".pin"} }

func (m *Manager) Exists() (bool, error) {
	_, err := os.Stat(m.path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (m *Manager) Set(pin string) error {
	return m.SetProfile(pin, "")
}

func (m *Manager) SetProfile(pin, name string) error {
	if !valid(pin) {
		return errors.New("PIN must contain exactly four digits")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("create PIN salt: %w", err)
	}
	digest := hash(salt, pin)
	b, err := json.Marshal(pinFile{Version: 1, Name: name, Salt: hex.EncodeToString(salt), Hash: hex.EncodeToString(digest)})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(m.path, append(b, '\n'), 0600)
}

func (m *Manager) Name() (string, error) {
	saved, err := m.read()
	if err != nil {
		return "", err
	}
	return saved.Name, nil
}

func (m *Manager) SetName(name string) error {
	saved, err := m.read()
	if err != nil {
		return err
	}
	saved.Name = name
	b, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, append(b, '\n'), 0600)
}

func (m *Manager) Verify(pin string) (bool, error) {
	saved, err := m.read()
	if err != nil {
		return false, err
	}
	salt, err := hex.DecodeString(saved.Salt)
	if err != nil {
		return false, fmt.Errorf("decode PIN salt: %w", err)
	}
	want, err := hex.DecodeString(saved.Hash)
	if err != nil {
		return false, fmt.Errorf("decode PIN hash: %w", err)
	}
	got := hash(salt, pin)
	return len(want) == len(got) && subtle.ConstantTimeCompare(want, got) == 1, nil
}

func (m *Manager) read() (pinFile, error) {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return pinFile{}, err
	}
	var saved pinFile
	if err := json.Unmarshal(b, &saved); err != nil {
		return pinFile{}, fmt.Errorf("decode PIN file: %w", err)
	}
	return saved, nil
}

func valid(pin string) bool {
	if len(pin) != 4 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hash(salt []byte, pin string) []byte {
	value := append(append([]byte{}, salt...), []byte(pin)...)
	sum := sha256.Sum256(value)
	return sum[:]
}
