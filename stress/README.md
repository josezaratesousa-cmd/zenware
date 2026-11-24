# /stress

Programa en Go que prueba la API REST de Stamping con actas electorales sintéticas y permite
ver el resultado en el visor. Sirve para demostrar que se realizó la prueba de estrés.

> **Atención:** cada transacción es real. Sella un registro, descuenta un crédito de evidencias de
> la cuenta configurada y dispara el anclaje en blockchain. No es una simulación.

## Qué hace

1. Se indica la cantidad de transacciones (1 a 500,000) y la concurrencia (1 a 200) en una interfaz web.
2. Por cada transacción genera un acta con resultados aleatorios que cuadran (la suma de opciones es
   igual a los votos válidos, y válidos más nulos más blancos es igual a los votantes).
3. Genera un PDF sintético, calcula su SHA-256 y lo deja en la custodia del visor.
4. Sella el acta con `POST {STAMPING_BASE_URL}/stamp/` (modo asíncrono, token en la cabecera `Authorization`).
5. Muestra el avance en vivo: enviadas, correctas, con error, transacciones por segundo y latencias p50 y p95.
6. Cada fila tiene un enlace al visor, y un botón abre el visor con la última acta sellada.
7. Al terminar guarda un reporte (`summary.json` y `results.csv`) en `RUNS_DIR`, descargable desde la interfaz.

## Ejecución

```bash
cd stress
go build -o stress .
cp .env.example /ruta/fuera/del/repositorio/stress.env     # completar según la sección siguiente
chmod 600 /ruta/fuera/del/repositorio/stress.env
STRESS_ENV_FILE=/ruta/fuera/del/repositorio/stress.env ./stress
```

La interfaz queda en `http://127.0.0.1:8090`. Por defecto solo escucha en localhost. Si se expone en
otra dirección, `STRESS_UI_PASSWORD` es obligatoria y la interfaz pide usuario cualquiera y esa contraseña.

## Credenciales y configuración

Ver [`.env.example`](.env.example). **El repositorio no contiene valores.**

| Variable | Obligatoria | Qué es |
|---|---|---|
| `STAMPING_TOKEN` | Sí | API key de la cuenta que sella (la cuenta Zenware). Va en la cabecera, nunca en la URL |
| `STAMPING_BASE_URL` | No | Por defecto `https://api.stamping.io` |
| `PROCESS_CODE`, `PROCESS_NAME` | No | Proceso con que se marcan las actas (`ZW-TEST` por defecto) |
| `STATION_BASE` | No | La mesa n es `STATION_BASE + n` |
| `VISOR_URL` | Para los enlaces | URL pública del visor de Zenware |
| `CUSTODY_PATH` | Para los enlaces | Carpeta de custodia local del visor; debe ser la misma del visor |
| `CODE_CIPHER_KEY` | Para los enlaces | Misma clave (32 bytes en hexadecimal) que usa el visor para descifrar el QR |
| `LISTEN_ADDR`, `STRESS_UI_PASSWORD` | No | Dirección de la interfaz y su contraseña |
| `RUNS_DIR` | No | Dónde se guardan los reportes |

Las tres variables del visor van juntas: sin ellas la prueba corre igual, pero sin enlaces.

## Visor para Zenware

`/stress` usa el proyecto [`../visor`](../visor) con su propia configuración, distinta de la de ONPE:

- `CUSTODY_DRIVER=local` y `CUSTODY_PATH` igual al de `/stress`.
- `CODE_CIPHER_KEY` igual a la de `/stress`.
- `EXPECTED_ISSUER_ID` con el identificador de la cuenta Zenware. Así el visor solo da por verificadas
  las actas selladas por esa cuenta.

El detalle de esas variables está en [`../visor/docs/credenciales.md`](../visor/docs/credenciales.md).

## Notas

- La API de Stamping entrega `code` como texto o como número según el endpoint; el programa acepta ambos.
- En modo asíncrono la respuesta no trae el `trxid`: el programa lo calcula como `sha1(sha256(pdf))`,
  que es como lo deriva Stamping.
- Los anclajes en LNET y Rollux aparecen en el visor cuando el cron de anclaje procesa el lote.
