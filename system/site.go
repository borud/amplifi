package system

import (
	"context"
	"fmt"
	"maps"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// SiteConfigOption codes.
const (
	siteID                    = "1"
	siteBandSteering          = "2"
	siteRouterSteering        = "3"
	siteChannel24             = "4"
	siteChannel5              = "5"
	siteGuestClientLimit      = "6"
	siteFirmwareUpdateChannel = "7"
	siteGuestDisableTime      = "8"
	siteGuestDurationLimit    = "9"
	siteCountry               = "10"
	siteTimezone              = "11"
	siteZoneName              = "12"
	siteMultipleHops          = "13"
	siteClockType             = "14"
	siteMlAdoptEmail          = "16"
	siteMlAdoptPhone          = "17"
	siteMlAdoptPin            = "18"
	siteWPSFromLCD            = "19"
	siteAcceptQoSOverhead     = "23"
	siteWebRTCDeviceID        = "24"
	siteWebRTCOwnerID         = "25"
	siteWebRTCAuthKey         = "26"
	siteWebRTCLicense         = "27"
	siteFriendClientLimit     = "28"
	siteFriendDisableTime     = "29"
	siteFriendDurationLimit   = "30"
	siteAcceptDFS             = "33"
)

// siteSecretNames names the site options holding remote access (Teleport)
// credentials, as they appear in SiteConfig.Secrets.
var siteSecretNames = map[string]string{
	siteMlAdoptEmail:   "ml_adopt_email",
	siteMlAdoptPhone:   "ml_adopt_phone",
	siteMlAdoptPin:     "ml_adopt_pin",
	siteWebRTCDeviceID: "webrtc_device_id",
	siteWebRTCOwnerID:  "webrtc_owner_id",
	siteWebRTCAuthKey:  "webrtc_auth_key",
	siteWebRTCLicense:  "webrtc_license",
}

// SiteConfig is the mesh-wide site configuration. Fields holds every option
// code returned, so unmapped ones are still visible. Secrets holds the
// remote access (Teleport) credentials; see Redacted. Channels use -1 for
// automatic selection.
type SiteConfig struct {
	SiteID                int64             `json:"site_id"`
	Country               string            `json:"country"`
	Timezone              string            `json:"timezone"`
	ZoneName              string            `json:"zone_name"`
	ClockType             int64             `json:"clock_type"`
	BandSteering          bool              `json:"band_steering"`
	RouterSteering        bool              `json:"router_steering"`
	MultipleHops          bool              `json:"multiple_hops"`
	Channel24             int64             `json:"channel_2_4ghz"`
	Channel5              int64             `json:"channel_5ghz"`
	FirmwareUpdateChannel string            `json:"firmware_update_channel"`
	WPSFromLCD            bool              `json:"wps_from_lcd"`
	AcceptQoSOverhead     bool              `json:"accept_qos_overhead"`
	AcceptDFS             bool              `json:"accept_dfs_radar_interruptions"`
	GuestClientLimit      int64             `json:"guest_client_limit"`
	GuestDisableTime      string            `json:"guest_disable_time"`
	GuestDurationLimit    int64             `json:"guest_duration_limit"`
	FriendClientLimit     int64             `json:"friend_client_limit"`
	FriendDisableTime     string            `json:"friend_disable_time"`
	FriendDurationLimit   int64             `json:"friend_duration_limit"`
	Secrets               map[string]string `json:"secrets"`
	Fields                map[string]any    `json:"fields"`
}

// Redacted returns a copy of s without the remote access credentials.
func (s SiteConfig) Redacted() SiteConfig {
	s.Secrets = map[string]string{}
	s.Fields = maps.Clone(s.Fields)
	for code := range siteSecretNames {
		delete(s.Fields, code)
	}
	return s
}

// Site reads the site configuration.
func Site(ctx context.Context, c *amplifi.Client) (*SiteConfig, error) {
	m, err := rpc.ClusterMap(ctx, c, "GetSiteConfig")
	if err != nil {
		return nil, fmt.Errorf("site config: %w", err)
	}
	return decodeSite(m), nil
}

func decodeSite(m map[string]any) *SiteConfig {
	s := &SiteConfig{
		SiteID:                rpc.Int(m, siteID),
		Country:               rpc.String(m, siteCountry),
		Timezone:              rpc.String(m, siteTimezone),
		ZoneName:              rpc.String(m, siteZoneName),
		ClockType:             rpc.Int(m, siteClockType),
		BandSteering:          rpc.Bool(m, siteBandSteering),
		RouterSteering:        rpc.Bool(m, siteRouterSteering),
		MultipleHops:          rpc.Bool(m, siteMultipleHops),
		Channel24:             rpc.Int(m, siteChannel24),
		Channel5:              rpc.Int(m, siteChannel5),
		FirmwareUpdateChannel: rpc.String(m, siteFirmwareUpdateChannel),
		WPSFromLCD:            rpc.Bool(m, siteWPSFromLCD),
		AcceptQoSOverhead:     rpc.Bool(m, siteAcceptQoSOverhead),
		AcceptDFS:             rpc.Bool(m, siteAcceptDFS),
		GuestClientLimit:      rpc.Int(m, siteGuestClientLimit),
		GuestDisableTime:      rpc.String(m, siteGuestDisableTime),
		GuestDurationLimit:    rpc.Int(m, siteGuestDurationLimit),
		FriendClientLimit:     rpc.Int(m, siteFriendClientLimit),
		FriendDisableTime:     rpc.String(m, siteFriendDisableTime),
		FriendDurationLimit:   rpc.Int(m, siteFriendDurationLimit),
		Secrets:               map[string]string{},
		Fields:                m,
	}
	for code, name := range siteSecretNames {
		if v := rpc.String(m, code); v != "" {
			s.Secrets[name] = v
		}
	}
	return s
}

// SiteUpdate is a change to the site configuration. Nil fields are left as
// they are. These are the writable options.
type SiteUpdate struct {
	// ZoneName is an IANA zone such as "America/Chicago".
	ZoneName            *string
	ClockType           *int
	WPSFromLCD          *bool
	AcceptQoSOverhead   *bool
	AcceptDFS           *bool
	GuestClientLimit    *int
	GuestDurationLimit  *int
	FriendClientLimit   *int
	FriendDurationLimit *int
}

// changes returns u as option code -> new value.
func (u SiteUpdate) changes() map[string]any {
	out := map[string]any{}
	rpc.Put(out, siteZoneName, u.ZoneName)
	rpc.Put(out, siteClockType, u.ClockType)
	rpc.Put(out, siteGuestClientLimit, u.GuestClientLimit)
	rpc.Put(out, siteGuestDurationLimit, u.GuestDurationLimit)
	rpc.Put(out, siteFriendClientLimit, u.FriendClientLimit)
	rpc.Put(out, siteFriendDurationLimit, u.FriendDurationLimit)
	rpc.Put(out, siteWPSFromLCD, u.WPSFromLCD)
	rpc.Put(out, siteAcceptQoSOverhead, u.AcceptQoSOverhead)
	rpc.Put(out, siteAcceptDFS, u.AcceptDFS)
	return out
}

// UpdateSite applies u to the site configuration: it reads the whole config,
// applies the changes and writes it back.
func UpdateSite(ctx context.Context, c *amplifi.Client, u SiteUpdate) error {
	changes := u.changes()
	if len(changes) == 0 {
		return nil
	}
	if err := rpc.UpdateCluster(ctx, c, "GetSiteConfig", "SetSiteConfig", changes); err != nil {
		return fmt.Errorf("update site config: %w", err)
	}
	return nil
}
