{{/* vim: set filetype=mustache: */}}

{{- define "rabbitmq.auth.secret.name" -}}
{{- default "steadybit-extension-rabbitmq" .Values.rabbitmq.auth.existingSecret -}}
{{- end -}}
