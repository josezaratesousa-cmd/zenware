# Credenciales y configuración

Este documento lista qué se necesita para que el visor funcione. **No contiene valores.**
La configuración se lee de un archivo `.env` ubicado **fuera del repositorio**, cuya ruta se
indica con la variable `APP_ENV_FILE`. La plantilla sin valores es `.env.example`.

Si falta `CODE_CIPHER_KEY`, la aplicación no arranca (falla a propósito en lugar de correr con
valores de relleno).

## Secretos

| Variable | Obligatoria | Para qué sirve | Cómo se obtiene |
|---|---|---|---|
| `CODE_CIPHER_KEY` | **Sí** | Descifra el código del QR (AES-256-GCM). Debe ser la misma con la que se generaron los códigos impresos | 32 bytes en hexadecimal: `.venv/bin/python -m app.services.code_cipher` |
| `STAMPING_TOKEN` | No, recomendada | Acota la consulta a la cuenta propia y recupera el registro legítimo si otra cuenta selló el mismo PDF. Alcanza con un token de **solo lectura** | Lo emite Stamping para la cuenta de la entidad |
| `CUSTODY_ACCESS_KEY` | Solo con `s3` y sin rol de instancia | Acceso al bucket que guarda los PDF | Usuario IAM con únicamente `s3:GetObject` sobre el prefijo. En AWS es preferible dejarla vacía y usar el rol de la instancia |
| `CUSTODY_SECRET_KEY` | Idem | Par de `CUSTODY_ACCESS_KEY` | Idem |

Un token de **escritura** de Stamping no pertenece a este servicio: lo necesita únicamente
`tools/register_record.py`, que se ejecuta desde consola y no desde el servidor público.

## Datos de configuración que no son secretos pero son obligatorios

| Variable | Qué es |
|---|---|
| `EXPECTED_ISSUER_ID` | Identificador de la cuenta que debe haber registrado el acta (`result.ownership.userId` de cualquier consulta a un acta ya registrada). Vacío desactiva la comprobación; no conviene en producción |
| `EXPECTED_ISSUER_NAME` | Nombre que se registra en el log |
| `CUSTODY_DRIVER` | `local` o `s3` |
| `CUSTODY_PATH` | Con `local`, carpeta raíz; con `s3`, prefijo dentro del bucket |
| `CUSTODY_BUCKET`, `CUSTODY_REGION`, `CUSTODY_ENDPOINT` | Solo con `s3`. El endpoint solo si no es AWS |
| `APP_BASE_URL` | URL pública del visor, usada en las etiquetas Open Graph |
| `APP_ROOT_PATH` | Prefijo si el visor se monta bajo un subdirectorio |

## Accesos de red y de infraestructura

| Qué | Detalle |
|---|---|
| Salida HTTPS a `STAMPING_BASE_URL` | Por defecto `https://api.stamping.io`. Si Stamping restringe por origen, hay que registrar la IP de salida del servidor |
| Nodo LNET propio (`LACCHAIN_RPC`) | Opcional. LACChain es público-permisionado: no hay endpoints abiertos y el nodo se obtiene registrándose ante la LACChain Alliance. Sin él, el anclaje LNET no se verifica por cuenta propia. Debe ser un nodo de la entidad, no del proveedor |
| `ROLLUX_RPC` | Por defecto `https://rpc.rollux.com`, público |
| `IPFS_GATEWAY` | Conviene una pasarela propia; una pública saca al ciudadano de la infraestructura y puede limitar o desaparecer |
| Proxy inverso con TLS | El visor escucha solo en `127.0.0.1` |

## Dónde viven los PDF

La custodia es el componente que se reemplaza en cada despliegue. Los PDF se esperan en
`<CUSTODY_PATH>/<identificador>.pdf` (driver `local`) o en
`s3://<CUSTODY_BUCKET>/<CUSTODY_PATH>/<identificador>.pdf` (driver `s3`). El **PDF debe ser el final,
ya firmado**, byte por byte igual al que se hasheó al registrar. Ver la sección 3 de
[`integration-guide.es.md`](integration-guide.es.md).

## Qué no debe pasar

- No versionar un `.env` lleno, ni dejarlo dentro del árbol del proyecto.
- No enviar el token por query string: queda en los logs de acceso.
- No registrar `CODE_CIPHER_KEY` en los logs ni compartirla por chat o correo.
- No versionar actas reales.
