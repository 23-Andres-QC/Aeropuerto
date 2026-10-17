package services

import (
	"fmt"
	"html"
)

// ResetMail is the recovery e-mail: a small HTML page with the LAP brand and the same text without markup. It carries the
// link and nothing else that is sensitive. name and link are escaped for the HTML.
func ResetMail(name, link string, minutes int) (htmlBody, text string) {
	n, l := html.EscapeString(name), html.EscapeString(link)
	htmlBody = fmt.Sprintf(`<!doctype html>
<html lang="es"><body style="margin:0;background:#eef3fb;font-family:Segoe UI,Arial,sans-serif;color:#0f172a">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="padding:24px 12px"><tr><td align="center">
<table role="presentation" width="480" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:14px;overflow:hidden;border:1px solid #dbe4f3">
<tr><td style="background:#0b3b8f;padding:18px 24px;color:#ffffff;font-size:18px;font-weight:700">LAP <span style="font-weight:400;font-size:12px;letter-spacing:1px">· LIMA AIRPORT PARTNERS</span></td></tr>
<tr><td style="padding:24px">
<p style="margin:0 0 12px;font-size:16px">Hola, %s.</p>
<p style="margin:0 0 16px;line-height:1.5">Recibimos una solicitud para restablecer tu contraseña. Pulsa el botón para elegir una nueva. El enlace sirve <b>una sola vez</b> y vence en %d minutos.</p>
<p style="margin:0 0 20px"><a href="%s" style="display:inline-block;background:#2563eb;color:#ffffff;text-decoration:none;padding:12px 22px;border-radius:10px;font-weight:600">Restablecer contraseña</a></p>
<p style="margin:0 0 8px;font-size:12px;color:#475569">Si el botón no funciona, copia este enlace en tu navegador:<br><span style="word-break:break-all">%s</span></p>
<p style="margin:16px 0 0;font-size:12px;color:#475569">Si no lo pediste tú, ignora este mensaje: tu contraseña sigue igual.</p>
</td></tr></table></td></tr></table></body></html>`, n, minutes, l, l)
	text = fmt.Sprintf("Hola, %s.\n\nRecibimos una solicitud para restablecer tu contraseña de LAP. Abre este enlace para elegir una nueva (sirve una sola vez y vence en %d minutos):\n\n%s\n\nSi no lo pediste tú, ignora este mensaje: tu contraseña sigue igual.\n", name, minutes, link)
	return htmlBody, text
}
