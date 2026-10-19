package webshell

import (
	"html/template"
	"net/http"
)

func (renderer *Renderer) RenderSystemSettings(w http.ResponseWriter, data AdminPageData) error {
	normalizeAdminPage(&data)
	data.ShowPageHeader = false
	content, err := executeTemplate(renderer.templates, "system_settings", data)
	if err != nil {
		return err
	}
	body, err := executeTemplate(renderer.templates, "admin_base", AdminShellView{AdminPageData: data, Content: template.HTML(content), SystemSettings: true})
	if err != nil {
		return err
	}
	return writeHTML(w, http.StatusOK, body)
}
