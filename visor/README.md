# Visor de actas

Verificador público de actas de escrutinio. Se escanea el código QR impreso en el acta,
se muestra el documento y se puede comprobar que es idéntico al que se selló en blockchain.

Este directorio es una copia del proyecto `visor_actas` y sirve de base para el piloto con
Zenware. Su README original está en [`docs/README-visor_actas.es.md`](docs/README-visor_actas.es.md).

- Credenciales que se necesitan: [`docs/credenciales.md`](docs/credenciales.md)
- Plantilla de configuración, sin valores: [`.env.example`](.env.example)
- Contrato de datos que debe enviar quien registra las actas:
  [`docs/integration-guide.es.md`](docs/integration-guide.es.md)

## Principio: el visor solo lee

El visor no puede registrar evidencia. El cliente que habla con la API de atestación rechaza
cualquier método HTTP distinto de `GET`, de modo que el código alcanzable desde una petición
no tiene camino para falsificar una atestación. El registro es una herramienta aparte
(`tools/register_record.py`) que se ejecuta desde consola y nunca desde la máquina que da a internet.

## Cómo lee los datos

Una visita recorre esta cadena. Cada paso nombra el archivo que lo hace.

```
código del QR → identificador → PDF en custodia → SHA-256 → TrxID → atestación → pantalla
```

1. **Entrada.** El QR apunta a `https://<dominio>/<código>`. El código debe cumplir
   `^[A-Za-z0-9_-]{6,120}$`; si no, responde 404 sin consultar nada.
   (`app/main.py`, `app/routers/records.py`)
2. **Descifrado.** `app/services/code_cipher.py` convierte el código en un identificador
   interno, por ejemplo `EMC-2026/035253`. Por defecto usa AES-256-GCM con la clave
   `CODE_CIPHER_KEY`. Un código manipulado se rechaza (`InvalidCode`) y se responde como
   «no encontrado», para no revelar qué códigos existen.
3. **Custodia.** `app/services/custody/` lee los bytes del PDF firmado, que es la única
   fuente del documento. Hay dos drivers, `local` (`<CUSTODY_PATH>/<identificador>.pdf`)
   y `s3` (`s3://<bucket>/<CUSTODY_PATH>/<identificador>.pdf`). Los identificadores se
   tratan como entrada hostil y pasan por `safe_identifier()`.
4. **Huella.** Se calcula el SHA-256 del PDF y de él el TrxID: `trxid = sha1(sha256(pdf))`.
5. **Atestación.** `app/services/stamping.py` hace `GET {STAMPING_BASE_URL}/stamp/get/?byTrxid=<trxid>`.
   Esa consulta es pública: solo envía una huella, que no revela nada por sí sola. El token es
   opcional y, si existe, viaja en la cabecera `X-API-Token`, nunca en la URL.
6. **Comprobaciones.** `app/services/record_service.py` exige que:
   - la evidencia registrada coincida con el SHA-256 del PDF que se tiene en custodia;
   - la cuenta que registró el acta sea `EXPECTED_ISSUER_ID`. Si no coincide, se responde
     igual que un acta en trámite y se deja un error en el log.
7. **Estado.** El resultado es uno de cuatro:

   | Estado | Cuándo |
   |---|---|
   | `verified` | PDF en custodia, evidencia registrada, coincide el hash y el emisor es el esperado |
   | `pending` | Falta el PDF, no hay registro todavía, falla la consulta o el emisor no coincide |
   | `altered` | Reservado en el modelo; hoy un hash distinto aparece como `pending`, porque un archivo alterado produce otro TrxID y no se encuentra |
   | `not_found` | Código inválido o ilegible |

8. **Qué se muestra.** Se decodifican dos campos que la entidad envió al registrar, ambos JSON
   en base64: `info` (resultados: electores hábiles, votantes, votos válidos, nulos, blancos y
   opciones con sus votos) e `data` (mesa, proceso, ubicación). Un campo que no se envió no se
   muestra: no hay valores por defecto ni se completa con otras fuentes. El contrato de ambos
   está en `app/models.py`, donde se validan los cuadres aritméticos de las actas.
9. **Anclajes.** De la respuesta se toman solo los que existen: IPFS, LNET (LACChain), Rollux
   y la raíz del árbol de Merkle de Stamping. Un anclaje sin valor se omite. Los enlaces a los
   exploradores salen de la configuración.
10. **Imágenes.** Las páginas del acta se renderizan en el servidor como WebP
    (`app/services/rendering.py`, PyMuPDF) y se guardan en caché por el hash del documento.

### Endpoints

Todos son `GET` y los atiende `app/routers/records.py`.

| Ruta | Devuelve |
|---|---|
| `/<código>` | La página, con las etiquetas Open Graph de esa acta |
| `/api/records/<código>` | El acta ya resuelta, lista para pintar |
| `/api/records/<código>/pdf` | El PDF firmado (`?download=1` para descargarlo) |
| `/api/records/<código>/pages/<n>` | La página `n` como imagen WebP (`?density=@2x`) |
| `/health` | Estado del servicio |

### Límites y caché

Un acta verificada se sirve de memoria `RECORD_CACHE_SECONDS` (60 s por defecto); las actas en
trámite no se cachean. Hay un límite de peticiones por dirección (`RATE_LIMIT_*`), que es solo
un respaldo detrás de la caché.

## Lo que el visor no hace hoy

- No valida la firma PAdES: solo informa si el PDF trae una firma (`unsigned` o `unverified`).
  La validación completa corresponde a quien registra el acta.
- No muestra el estado independiente de cada red ni un estado agregado (pendiente, parcial,
  completa, fallida). Muestra los anclajes que la respuesta ya trae.
- La verificación directa contra las cadenas (`tools/verify_chain.py`) es una herramienta de
  consola; la pantalla no la ejecuta.
- No incluye panel de monitoreo ni mapa de avance de carga.

## Ejecución local

```bash
python3.11 -m venv .venv
.venv/bin/pip install -r requirements.txt
cp .env.example /ruta/fuera/del/repositorio/.env     # completar según docs/credenciales.md
chmod 600 /ruta/fuera/del/repositorio/.env
APP_ENV_FILE=/ruta/fuera/del/repositorio/.env \
  .venv/bin/uvicorn app.main:app --host 127.0.0.1 --port 8081
```

La aplicación escucha solo en localhost; el TLS lo termina el proxy inverso. Si se monta bajo un
subdirectorio, ese prefijo va en `APP_ROOT_PATH`.

## Estructura

```
app/        backend FastAPI (config, modelos, routers, servicios, custodia)
web/        frontend: HTML, CSS y módulos ES, sin framework ni compilación
tools/      scripts de operador; el único que escribe en la API es register_record.py
tests/      pruebas, con actas sintéticas en tests/fixtures
docs/       guías de integración y credenciales
```

## Seguridad del repositorio

- No se versiona ningún `.env`, clave ni token; `.gitignore` los excluye.
- Las actas de `tests/fixtures` son **sintéticas**. Un acta real lleva en su firma PAdES los
  nombres y documentos de los miembros de mesa: no debe versionarse nunca.
