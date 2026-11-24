package main

import (
	"encoding/base64"
	"encoding/json"
)

type Disk struct {
	Model     string `json:"model,omitempty"`
	Bus       string `json:"bus"`     // sata, nvme, usb, ata, sas, raid, sd, other
	SSD       *bool  `json:"ssd"`     // nil = unknown
	SizeGB    int    `json:"size_gb"` // decimal GB, 0 = unknown
	Removable bool   `json:"removable,omitempty"`
	PCIe      *PCIe  `json:"pcie,omitempty"` // NVMe only; nil = unknown
}

// Payload is everything that leaves the program. Keep it a whitelist.
type Payload struct {
	V int `json:"v"`
	SMBIOS
	CPU   string `json:"cpu,omitempty"`
	OS    string `json:"os,omitempty"`
	Disks []Disk `json:"disks"`
}

func encode(p Payload) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
