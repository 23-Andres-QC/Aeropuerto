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
<tr><td style="background:#0b3b8f;padding:6px 24px;color:#ffffff"><table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td style="font-size:18px;font-weight:700;color:#ffffff">LAP <span style="font-weight:400;font-size:12px;letter-spacing:1px">· LIMA AIRPORT PARTNERS</span></td><td align="right"><img src="cid:maru" width="110" alt="Maru, la mascota de LAP" style="display:block;border:0;width:110px;height:auto"></td></tr></table></td></tr>
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

// PasswordChangedMail confirms that the password of an account was changed: when (Lima time, already formatted) and a warning to
// act if it was not its owner. It carries no link and nothing sensitive. name and when are escaped for the HTML.
func PasswordChangedMail(name, when string) (htmlBody, text string) {
	n, w := html.EscapeString(name), html.EscapeString(when)
	htmlBody = fmt.Sprintf(`<!doctype html>
<html lang="es"><body style="margin:0;background:#eef3fb;font-family:Segoe UI,Arial,sans-serif;color:#0f172a">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="padding:24px 12px"><tr><td align="center">
<table role="presentation" width="480" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:14px;overflow:hidden;border:1px solid #dbe4f3">
<tr><td style="background:#0b3b8f;padding:6px 24px;color:#ffffff"><table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td style="font-size:18px;font-weight:700;color:#ffffff">LAP <span style="font-weight:400;font-size:12px;letter-spacing:1px">· LIMA AIRPORT PARTNERS</span></td><td align="right"><img src="cid:maru" width="110" alt="Maru, la mascota de LAP" style="display:block;border:0;width:110px;height:auto"></td></tr></table></td></tr>
<tr><td style="padding:24px">
<p style="margin:0 0 12px;font-size:16px">Hola, %s.</p>
<p style="margin:0 0 16px;line-height:1.5"><b>Tu contraseña se cambió con éxito</b> el %s (hora de Lima). Ya puedes entrar con la nueva.</p>
<p style="margin:0;font-size:12px;color:#475569">Por seguridad, cerramos tus otras sesiones abiertas. Si no fuiste tú, avisa de inmediato a quien administra la plataforma para que bloquee tu cuenta.</p>
</td></tr></table></td></tr></table></body></html>`, n, w)
	text = fmt.Sprintf("Hola, %s.\n\nTu contraseña de LAP se cambió con éxito el %s (hora de Lima). Ya puedes entrar con la nueva.\n\nPor seguridad, cerramos tus otras sesiones abiertas. Si no fuiste tú, avisa de inmediato a quien administra la plataforma para que bloquee tu cuenta.\n", name, when)
	return htmlBody, text
}
