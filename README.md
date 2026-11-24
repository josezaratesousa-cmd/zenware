# Zenware

Material del piloto de atestación de actas electorales entre STAMPING S.A.C. y ZENWARE S.A.C.
(Cotización N.° 02112026 - STAMPING).

| Carpeta | Contenido |
|---|---|
| [`stress/`](stress/) | Programa en Go para probar la API de Stamping con actas sintéticas y ver los resultados en el visor. Ver [`stress/README.md`](stress/README.md). |
| [`visor/`](visor/) | Visor público de actas: el ciudadano escanea el QR del acta, la ve y comprueba su integridad contra blockchain. Ver [`visor/README.md`](visor/README.md). |

El repositorio no contiene secretos. Las credenciales que necesita el visor están
documentadas, sin valores, en [`visor/docs/credenciales.md`](visor/docs/credenciales.md).
