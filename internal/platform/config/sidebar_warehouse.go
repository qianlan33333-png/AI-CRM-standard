package config

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// SidebarWarehouse uses an existing protected operator account without
// provisioning a user or granting a role. Read and manual generation are
// separately enabled; neither path sends a WeCom message.
type SidebarWarehouse struct {
	DatabaseProfileEnabled bool
	Enabled                bool
	GenerationEnabled      bool
	Origin                 string
	Username               string
	Password               string
	// Test injection only. Load never sets a custom trust/transport client.
	HTTPClient *http.Client
}

func loadSidebarWarehouse() (SidebarWarehouse, error) {
	c := SidebarWarehouse{Origin: os.Getenv("AICRM_SIDEBAR_WAREHOUSE_ORIGIN"), Username: os.Getenv("AICRM_SIDEBAR_WAREHOUSE_BASIC_USER"), Password: os.Getenv("AICRM_SIDEBAR_WAREHOUSE_BASIC_PASSWORD")}
	var err error
	if c.DatabaseProfileEnabled, err = strictBool("AICRM_SIDEBAR_PROFILE_DATABASE_ENABLED", false); err != nil {
		return c, err
	}
	if c.Enabled, err = strictBool("AICRM_SIDEBAR_WAREHOUSE_ENABLED", false); err != nil {
		return c, err
	}
	if c.GenerationEnabled, err = strictBool("AICRM_SIDEBAR_GENERATION_ENABLED", false); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func (c SidebarWarehouse) Validate() error {
	if c.DatabaseProfileEnabled {
		if c.GenerationEnabled {
			return errors.New("database profile generation requires separate configuration")
		}
		return nil
	}
	if c.GenerationEnabled && !c.Enabled {
		return errors.New("sidebar generation requires the protected warehouse reader")
	}
	if !c.Enabled {
		return nil
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || c.Username == "" || strings.TrimSpace(c.Username) != c.Username || strings.ContainsAny(c.Username, ":\r\n") || c.Password == "" {
		return errors.New("invalid protected sidebar warehouse configuration")
	}
	return nil
}

func (c SidebarWarehouse) ValidateDatabaseProfile(sourceDSN, scope string) error {
	if !c.DatabaseProfileEnabled {
		return nil
	}
	if sourceDSN == "" || strings.TrimSpace(sourceDSN) != sourceDSN || !strings.HasPrefix(scope, "wechat-open-platform:") || len(scope) <= len("wechat-open-platform:") || strings.TrimSpace(scope) != scope {
		return errors.New("sidebar database profile requires existing HXC source and verified identity scope")
	}
	return nil
}
