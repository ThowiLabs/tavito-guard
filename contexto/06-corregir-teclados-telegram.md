# Fecha
2026-11-05
# Objetivo
Corregir fallos al pulsar Recursos e Historial y al enviar mensajes sin botones.
# Decisiones tomadas
No enviar reply_markup en sendMessage/sendDocument/sendPhoto si no hay botones. Al editar un mensaje, serializar un teclado vacío como inline_keyboard:[] para retirar botones existentes. Nunca serializar filas nulas.
# Arquitectura actual
Mismo cliente HTTP, sin nuevas dependencias. Keyboard normaliza el JSON y APIError conserva una descripción acotada y con credenciales ocultas.
# Librerías usadas
Sin cambios. Contrato consultado: https://core.telegram.org/bots/api#inlinekeyboardmarkup.
# Archivos importantes modificados
internal/guard/telegram.go, telegram_test.go, integration_test.go, cmd/tavito-guard/main.go y Makefile.
# Problemas encontrados
Keyboard{} enviaba inline_keyboard:null. La simulación anterior aceptaba ese tipo inválido, de modo que no detectó el rechazo real esperado por el contrato de Telegram. La prueba estricta falló antes de corregir el cliente: Recursos e Historial no enviaban imágenes.
# Soluciones implementadas
Arreglo JSON vacío, omisión de markup innecesario y descripciones de API sanitizadas. API simulada estricta; prueba de envío de foto de Recursos y del recorrido general. Versión 0.1.2.
# Pendientes
El usuario no compartió el texto exacto del fallo actual. Confirmar el resultado con su bot real tras reemplazar el binario.
# Próximos pasos
Publicar y entregar 0.1.2 con autoría ThowiLabs, preservando la corrección OpenRC de 0.1.1.
