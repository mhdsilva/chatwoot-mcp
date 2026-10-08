# Site público

Landing page estática do Chatwoot MCP. Ela é independente do painel local que fica em `web/` e inclui uma demonstração interativa com dados fictícios.

Para visualizar localmente, sirva a raiz do repositório e abra `/site/`:

```sh
python3 -m http.server 4173
```

Não há etapa de build. Publique o conteúdo de `site/` como arquivos estáticos.
