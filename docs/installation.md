# Instalação e desinstalação

O `chatwoot-mcp` é distribuído como aplicativo de bandeja. Os pacotes ficam na página de [releases](https://github.com/mhdsilva/chatwoot-mcp/releases). Cada release traz:

- `chatwoot-mcp_<versão>_darwin_<arch>.dmg` — app para macOS (Intel e Apple Silicon);
- `chatwoot-mcp_<versão>_windows_<arch>.exe` — instalador Inno Setup;
- `chatwoot-mcp_<versão>_linux_<arch>.deb` e `...rpm` — pacotes Linux;
- os binários soltos, para quem prefere executar direto.

O aplicativo inicia o painel local em `127.0.0.1:8765`, abre o navegador e mostra um ícone de bandeja com **Abrir painel** e **Sair**. Se a sessão gráfica não existir (servidor headless), o app continua rodando sem bandeja e informa a URL do painel.

## macOS

1. Baixe o `.dmg` da sua arquitetura (`arm64` para Apple Silicon, `amd64` para Intel).
2. Abra o `.dmg` e arraste **Chatwoot MCP** para **Aplicações**.
3. Na primeira execução, o macOS pode avisar que o app é de um desenvolvedor não identificado. O pacote é assinado apenas de forma ad-hoc (sem notarização); para abrir, use **Abrir mesmo assim** em Ajustes › Privacidade e Segurança, ou clique com o botão direito no app e escolha **Abrir**.

O app é um agente de menu (`LSUIElement`): não aparece no Dock.

Desinstalar: feche pelo menu **Sair** e mova o app de **Aplicações** para a Lixeira.

## Windows

1. Baixe o instalador `.exe`.
2. Execute-o. A instalação é por usuário (não exige administrador). O assistente oferece:
   - **Iniciar com o Windows** (opcional, desmarcado por padrão);
   - **Registrar no Claude Desktop** e **Registrar no Codex** (opcionais, desmarcados por padrão).
3. Ao final, o app pode abrir automaticamente.

O Windows SmartScreen pode alertar sobre um aplicativo não assinado; escolha **Mais informações › Executar assim mesmo**. A assinatura de código (Authenticode) é um passo futuro.

Desinstalar: **Configurações › Aplicativos**, ou o atalho **Desinstalar Chatwoot MCP** no menu Iniciar.

## Linux

### Debian/Ubuntu

```sh
sudo apt install ./chatwoot-mcp_<versão>_linux_amd64.deb
```

### Fedora/RHEL

```sh
sudo rpm -i chatwoot-mcp-<versão>-1.x86_64.rpm
```

O pacote instala o binário em `/usr/bin/chatwoot-mcp`, um atalho em `chatwoot-mcp.desktop` e o ícone. A bandeja usa o protocolo D-Bus StatusNotifierItem; no GNOME pode ser necessária uma extensão de bandeja (por exemplo, AppIndicator) para o ícone aparecer. Sem extensão, o painel continua acessível por `chatwoot-mcp app` e pela URL informada.

Desinstalar:

```sh
sudo apt remove chatwoot-mcp      # Debian/Ubuntu
sudo rpm -e chatwoot-mcp          # Fedora/RHEL
```

## Registrar no cliente MCP

O registro pode ser feito pelo painel (botões **Instalar no Claude Desktop** / **Instalar no Codex**) ou pela linha de comando:

```sh
chatwoot-mcp configure-client --client claude   # ou codex, ou all
chatwoot-mcp configure-client --client all --dry-run
chatwoot-mcp configure-client --client all --yes
```

O comando mostra o que será alterado e pede confirmação, a menos que `--yes` seja usado. Um backup é criado antes de qualquer escrita, e servidores, chaves e comentários existentes são preservados.

Arquivos usados:

| Cliente | macOS | Windows | Linux |
|---|---|---|---|
| Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` | `%APPDATA%\Claude\claude_desktop_config.json` | `~/.config/Claude/claude_desktop_config.json` |
| Codex | `~/.codex/config.toml` | `%USERPROFILE%\.codex\config.toml` | `~/.codex/config.toml` |

Depois de registrar, reinicie o cliente MCP.

## Compilar e empacotar localmente

```sh
make build     # bin/chatwoot-mcp
make test      # go test ./...
make dist      # binários para macOS, Windows e Linux, sem CGO
```

Para gerar os instaladores:

```sh
# Linux (.deb/.rpm), requer nfpm e um binário em dist/chatwoot-mcp
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/chatwoot-mcp ./cmd/chatwoot-mcp
ARCH=amd64 VERSION=0.1.0 nfpm package --config packaging/nfpm.yaml --packager deb --target dist/chatwoot-mcp.deb

# macOS (.dmg), executar no macOS
bash packaging/macos/build-dmg.sh 0.1.0 arm64 dist/chatwoot-mcp dist

# Windows (.exe), executar no Windows com Inno Setup 6
ISCC.exe /DAppVersion=0.1.0 /DARCH=amd64 packaging\windows\chatwoot-mcp.iss
```

O workflow [release](../.github/workflows/release.yml) faz o mesmo automaticamente ao publicar uma tag `v*`.

## Dados locais

A configuração de conexão fica em `chatwoot-mcp/config.json` no diretório de configuração do usuário (em Unix, normalmente sob `~/.config`), com permissão restrita. O token não é exibido pelo painel nem gravado em logs. Para remover os dados locais, apague esse diretório. Detalhes em [configuration.md](configuration.md).
