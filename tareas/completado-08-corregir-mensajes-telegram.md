# Corregir mensajes de Telegram

Revisar fallos al pulsar Recursos y otros botones. Se detectó que Keyboard{} serializa inline_keyboard:null en mensajes sin botones; el contrato exige un arreglo. Hacer estricta la API simulada para reproducir el rechazo, corregir JSON vacío y mostrar el motivo de errores de API con credenciales ocultas. Mantener autoría ThowiLabs.
