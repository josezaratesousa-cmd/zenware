<?php
/**
 * Puerta de /stress (piloto Zenware): detecta la sesión de Stamping.
 * Sin sesión -> login de Stamping con retorno a esta puerta (next=stress).
 * Con sesión y usuario permitido -> sirve el programa de prueba (proxy a 127.0.0.1:8090). Archivo nuevo, no modifica el panel.
 * SSO_SECRET debe coincidir con el de stress.env (aquí va el valor real, no en el repo).
 */
define('PANEL_SYSTEM', true);
require_once __DIR__ . '/config/config.php';
require_once __DIR__ . '/includes/database.php';
require_once __DIR__ . '/includes/auth.php';

const SSO_SECRET = '__SSO_SECRET__';

Auth::initSession();
if (!Auth::validateFromURL()) {
    $_SESSION = [];
    header('Location: ' . AUTH_URL . '/?next=stress');
    exit;
}

$allowed = ['zenware@stamping.io', 'jose.zarate@stamping.io'];
$row = Database::getInstance()->fetchOne('SELECT fireUser FROM users WHERE userPass = ? LIMIT 1', [(string)Auth::getUserId()]);
$name = strtolower(trim((string)($row->fireUser ?? '')));
if (!in_array($name, $allowed, true)) {
    http_response_code(403);
    echo 'Acceso restringido.';
    exit;
}

// Sesión válida y usuario permitido: se reenvía al programa local (127.0.0.1:8090).
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
