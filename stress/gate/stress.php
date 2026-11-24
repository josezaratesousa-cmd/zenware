<?php
/**
 * Puerta de /stress (piloto Zenware): sin login. Reenvía al programa de prueba
 * local (127.0.0.1:8090). La seguridad es el token del API REST, que la persona
 * escribe en la pantalla: sin un token válido no se puede sellar nada.
 * SSO_SECRET debe coincidir con el de stress.env (aquí va el valor real, no en el repo).
 */
const SSO_SECRET = '__SSO_SECRET__';
$name = 'publico';

$path = isset($_GET['p']) ? ltrim((string)$_GET['p'], '/') : '';
$qs = $_GET; unset($qs['p'], $qs['auth'], $qs['address']);
$ch = curl_init('http://127.0.0.1:8090/' . $path . ($qs ? '?' . http_build_query($qs) : ''));
$hdr = ['X-Gate-Secret: ' . SSO_SECRET, 'X-Stamping-User: ' . $name];
if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    curl_setopt($ch, CURLOPT_POST, true);
    curl_setopt($ch, CURLOPT_POSTFIELDS, file_get_contents('php://input'));
    $hdr[] = 'Content-Type: ' . ($_SERVER['CONTENT_TYPE'] ?? 'application/json');
}
curl_setopt_array($ch, [CURLOPT_HTTPHEADER => $hdr, CURLOPT_RETURNTRANSFER => true,
    CURLOPT_HEADER => true, CURLOPT_TIMEOUT => 60]);
$res = curl_exec($ch);
$code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
$hs = curl_getinfo($ch, CURLINFO_HEADER_SIZE);
curl_close($ch);
if ($res === false) { http_response_code(502); echo 'Servicio no disponible.'; exit; }
http_response_code($code ?: 502);
foreach (explode("\r\n", substr($res, 0, $hs)) as $h) {
    if (preg_match('/^(Content-Type|Content-Disposition|Cache-Control):/i', $h)) header($h);
}
echo substr($res, $hs);
