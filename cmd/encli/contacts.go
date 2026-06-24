package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Contact struct {
	Nickname   string `json:"nickname"`
	DeviceID   string `json:"device_id"`
	ServerAddr string `json:"server_addr,omitempty"`
}

type ContactsStore struct {
	path     string
	contacts []Contact
}

func NewContactsStore() *ContactsStore {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return &ContactsStore{
		path: filepath.Join(home, ".encli", "contacts.json"),
	}
}

func (cs *ContactsStore) Load() error {
	data, err := os.ReadFile(cs.path)
	if err != nil {
		if os.IsNotExist(err) {
			cs.contacts = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &cs.contacts)
}

func (cs *ContactsStore) Save() error {
	data, err := json.MarshalIndent(cs.contacts, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(cs.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(cs.path, data, 0600)
}

func (cs *ContactsStore) Add(nickname, deviceID string) error {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || deviceID == "" {
		return fmt.Errorf("nickname and device ID required")
	}
	for i, c := range cs.contacts {
		if c.DeviceID == deviceID {
			cs.contacts[i].Nickname = nickname
			return cs.Save()
		}
	}
	cs.contacts = append(cs.contacts, Contact{Nickname: nickname, DeviceID: deviceID})
	return cs.Save()
}

func (cs *ContactsStore) Delete(nickname string) error {
	for i, c := range cs.contacts {
		if strings.EqualFold(c.Nickname, nickname) {
			cs.contacts = append(cs.contacts[:i], cs.contacts[i+1:]...)
			return cs.Save()
		}
	}
	return nil
}

func (cs *ContactsStore) Search(query string) []Contact {
	query = strings.ToLower(query)
	if query == "" {
		result := make([]Contact, len(cs.contacts))
		copy(result, cs.contacts)
		return result
	}
	var result []Contact
	for _, c := range cs.contacts {
		if strings.Contains(strings.ToLower(c.Nickname), query) ||
			strings.Contains(c.DeviceID, query) {
			result = append(result, c)
		}
	}
	return result
}

func (cs *ContactsStore) Name() string {
	for _, c := range cs.contacts {
		if c.ServerAddr != "" {
			return c.Nickname
		}
	}
	if len(cs.contacts) > 0 {
		return cs.contacts[0].Nickname
	}
	return "not set"
}

func (cs *ContactsStore) Resolve(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("empty query")
	}
	for _, c := range cs.contacts {
		if strings.EqualFold(c.Nickname, query) || c.DeviceID == query {
			return c.DeviceID, nil
		}
	}
	return query, nil
}
