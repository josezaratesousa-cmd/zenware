<?php
/**
 * Puerta de /stress (piloto Zenware): detecta la sesión de Stamping.
 * Sin sesión -> login de Stamping con retorno a esta puerta (next=stress).
 * Con sesión y usuario permitido -> emite un token firmado de corta vida y
 * vuelve a https://manyao.pe/zenware-stress/ . Archivo nuevo, no modifica el panel.
 * SSO_SECRET debe coincidir con el de stress.env (aquí va el valor real, no en el repo).
 */
define('PANEL_SYSTEM', true);
require_once __DIR__ . '/config/config.php';
require_once __DIR__ . '/includes/database.php';
require_once __DIR__ . '/includes/auth.php';

const SSO_SECRET = '__SSO_SECRET__';
const RETURN_URL = 'https://manyao.pe/zenware-stress/';

Auth::initSession();
if (!Auth::validateFromURL()) {
    $_SESSION = [];
    header('Location: ' . AUTH_URL . '/?next=stress');
    exit;
}

$allowed = ['zenware@stamping.io', 'jose.zarate@stamping.io'];
$u = Auth::getUser();
$name = strtolower(trim((string)($u['userName'] ?? '')));
if (!in_array($name, $allowed, true)) {
    http_response_code(403);
    echo 'Acceso restringido.';
    exit;
}

$payload = rtrim(strtr(base64_encode($name . '|' . (time() + 8 * 3600)), '+/', '-_'), '=');
$tok = $payload . '.' . hash_hmac('sha256', $payload, SSO_SECRET);
header('Cache-Control: no-store');
header('Location: ' . RETURN_URL . '?t=' . $tok);
