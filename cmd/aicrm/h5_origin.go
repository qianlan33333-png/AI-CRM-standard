package main

import platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"

func h5PublicOrigin(cfg platformconfig.Runtime) string {
	if cfg.H5PublicOrigin != "" {
		return cfg.H5PublicOrigin
	}
	return cfg.PublicOrigin
}
