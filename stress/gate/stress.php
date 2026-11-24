<?php
/**
 * Puerta de /stress (piloto Zenware): exige sesión de Stamping y reenvía
 * al programa Go local (127.0.0.1:8090). Archivo nuevo, no modifica el panel.
 */
define('PANEL_SYSTEM', true);
require_once __DIR__ . '/config/config.php';
require_once __DIR__ . '/includes/database.php';
require_once __DIR__ . '/includes/auth.php';

Auth::initSession();
Auth::requireAuth(); // sin sesión: redirige al login de Stamping

$allowed = ['zenware@stamping.io', 'jose.zarate@stamping.io'];
$u = Auth::getUser();
$name = strtolower(trim((string)($u['userName'] ?? '')));
if (!in_array($name, $allowed, true)) {
    http_response_code(403);
    echo 'Acceso restringido.';
    exit;
}

$path = isset($_GET['p']) ? ltrim((string)$_GET['p'], '/') : '';
$qs = $_GET; unset($qs['p'], $qs['auth'], $qs['address']);
$url = 'http://127.0.0.1:8090/' . $path . ($qs ? '?' . http_build_query($qs) : '');
$ch = curl_init($url);
$hdr = ['X-Stamping-User: ' . $name];
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
