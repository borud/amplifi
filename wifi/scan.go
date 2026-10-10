package wifi

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// ScanResult is one station seen by the signal quality scan.
type ScanResult struct {
	Band    string      `json:"band"`
	Network string      `json:"network"`
	MAC     amplifi.MAC `json:"mac"`
	Quality int         `json:"quality"`
}

// StartScan starts the Wi-Fi signal quality scan
// (Device.StartWifiSignalQualityScan, with a msgpack nil argument).
func StartScan(ctx context.Context, c *amplifi.Client) error {
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "StartWifiSignalQualityScan", rpc.Null); err != nil {
		return fmt.Errorf("start scan: %w", err)
	}
	return nil
}

// StopScan stops the Wi-Fi signal quality scan
// (Device.StopWifiSignalQualityScan).
func StopScan(ctx context.Context, c *amplifi.Client) error {
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "StopWifiSignalQualityScan", nil); err != nil {
		return fmt.Errorf("stop scan: %w", err)
	}
	return nil
}

// ScanResults fetches the scan results (Device.GetWifiScanSignalQuality,
// with a [nil, nil] argument), sorted by band, network and MAC.
func ScanResults(ctx context.Context, c *amplifi.Client) ([]ScanResult, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetWifiScanSignalQuality", rpc.TwoNulls)
	if err != nil {
		return nil, fmt.Errorf("wifi scan: %w", err)
	}
	return parseScan(m), nil
}

// parseScan decodes a WifiClientBandMap: band -> network role -> binary MAC
// -> signal quality.
func parseScan(m map[string]any) []ScanResult {
	entries := []ScanResult{}
	for band, roles := range m {
		for role, stations := range rpc.Submaps(roles) {
			entries = appendStations(entries, band, role, stations)
		}
	}

	slices.SortFunc(entries, func(a, b ScanResult) int {
		return cmp.Or(
			cmp.Compare(a.Band, b.Band),
			cmp.Compare(a.Network, b.Network),
			cmp.Compare(a.MAC.String(), b.MAC.String()),
		)
	})
	return entries
}

// appendStations adds the stations (binary MAC -> quality) seen on one band
// and network.
func appendStations(entries []ScanResult, band, role string, stations map[string]any) []ScanResult {
	for mac := range stations {
		entries = append(entries, ScanResult{
			Band:    rpc.KeyName(rpc.BandNames, band),
			Network: rpc.KeyName(rpc.NetworkNames, role),
			MAC:     rpc.MAC(mac),
			Quality: int(rpc.Int(stations, mac)),
		})
	}
	return entries
}

// StartWPS starts WPS push-button pairing (Device.WpsPushButtonActivate).
func StartWPS(ctx context.Context, c *amplifi.Client) error {
	if _, err := c.Call(ctx, amplifi.IfaceDevice, "WpsPushButtonActivate", nil); err != nil {
		return fmt.Errorf("start wps: %w", err)
	}
	return nil
}

// StopWPS stops WPS push-button pairing (Device.WpsPushButtonCancel).
func StopWPS(ctx context.Context, c *amplifi.Client) error {
	if _, err := c.Call(ctx, amplifi.IfaceDevice, "WpsPushButtonCancel", nil); err != nil {
		return fmt.Errorf("stop wps: %w", err)
	}
	return nil
}

// GetWPSStatus reports the WPS push-button state (Device.WpsPushButtonStatus)
// as the unit's JSON answer.
func GetWPSStatus(ctx context.Context, c *amplifi.Client) (map[string]any, error) {
	var out map[string]any
	if err := rpc.Value(ctx, c, amplifi.IfaceDevice, "WpsPushButtonStatus", nil, &out); err != nil {
		return nil, fmt.Errorf("wps status: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
