# Corregir instalación OpenRC

El servidor Dex del usuario validó token y guardó configuración, pero svcforge rechazó RestartOnFailure antes de iniciar. Detectar el gestor real: OnFailure solo en systemd; Never en OpenRC según el contrato de la revisión fijada. Conservar configuración. Añadir regresión de ambas políticas y del backend OpenRC, compilar 0.1.1 y publicar con autoría ThowiLabs.
