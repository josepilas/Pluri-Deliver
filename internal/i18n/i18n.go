// Package i18n provides the small set of strings used on the Go side
// (system toast notifications) in English and Brazilian Portuguese.
package i18n

var strings_ = map[string]map[string]string{
	"en": {
		"test_title":       "Pluri-Deliver",
		"test_body":        "Notifications are working! This is how update alerts will look.",
		"plus_more_events": "%s (+%d more events)",
		"delivered_body":   "Your package has been delivered!",
		"hidden_title":     "Pluri-Deliver keeps running",
		"hidden_body":      "Still watching your packages in the background. Click the Pluri-Deliver tray icon to open the window again, or use \"Close application\" to exit.",
		"tray_open":        "Open Pluri-Deliver",
		"tray_refresh":     "Refresh all",
		"tray_quit":        "Close application",
	},
	"pt-BR": {
		"test_title":       "Pluri-Deliver",
		"test_body":        "Notificações funcionando! É assim que os alertas de atualização aparecerão.",
		"plus_more_events": "%s (+%d outros eventos)",
		"delivered_body":   "Sua encomenda foi entregue!",
		"hidden_title":     "O Pluri-Deliver continua rodando",
		"hidden_body":      "Ainda acompanhando suas encomendas em segundo plano. Clique no ícone do Pluri-Deliver na bandeja para abrir a janela, ou use \"Fechar aplicativo\" para sair.",
		"tray_open":        "Abrir Pluri-Deliver",
		"tray_refresh":     "Atualizar todos",
		"tray_quit":        "Fechar aplicativo",
	},
}

// T translates a key for the given language ("en" fallback).
func T(lang, key string) string {
	if lang != "pt-BR" {
		lang = "en"
	}
	if m, ok := strings_[lang]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return key
}
