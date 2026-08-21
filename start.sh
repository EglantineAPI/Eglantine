#!/usr/bin/env bash
# start.sh - baixa dependencias, libera a porta UDP, compila e inicia o servidor.
set -euo pipefail

cd "$(dirname "$0")"

CONFIG="config.toml"
BINARY="server-executable"

log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }

# --- porta -----------------------------------------------------------------
# config.toml e a fonte unica da verdade; o default do Dragonfly e o fallback.
PORT=""
if [ -f "$CONFIG" ]; then
	PORT=$(sed -n "s/^[[:space:]]*Address[[:space:]]*=[[:space:]]*['\"][^:]*:\([0-9]\{1,\}\)['\"].*/\1/p" "$CONFIG" | head -1)
fi
PORT="${PORT:-19130}"
log "Porta UDP: $PORT"

# --- dependencias ----------------------------------------------------------
command -v go >/dev/null 2>&1 || { warn "Go nao encontrado no PATH. Instale o Go 1.26+."; exit 1; }
log "Baixando dependencias..."
go mod download

# --- compilacao ------------------------------------------------------------
# No Android, wlynxg/anet (via pion/ice, do transporte NetherNet da
# gophertunnel) faz //go:linkname para net.zoneCache e o linker rejeita.
LDFLAGS=""
if [ "$(go env GOOS)" = "android" ]; then
	LDFLAGS="-checklinkname=0"
	warn "GOOS=android: compilando com -ldflags=$LDFLAGS"
fi

log "Compilando..."
if [ -n "$LDFLAGS" ]; then
	go build -ldflags="$LDFLAGS" -o "$BINARY" .
else
	go build -o "$BINARY" .
fi

# --- firewall --------------------------------------------------------------
# Abre a porta UDP no primeiro firewall reconhecido. Idempotente: nao duplica
# regras. Sem privilegio, apenas avisa em vez de falhar.
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then
		SUDO="sudo"
	fi
fi

open_port() {
	if [ "$(id -u)" -ne 0 ] && [ -z "$SUDO" ]; then
		warn "Sem root e sem sudo; pulando a liberacao da porta."
		return 0
	fi

	if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q "Status: active"; then
		log "ufw: liberando $PORT/udp"
		$SUDO ufw allow "$PORT"/udp
		return 0
	fi

	if command -v firewall-cmd >/dev/null 2>&1 && $SUDO firewall-cmd --state >/dev/null 2>&1; then
		log "firewalld: liberando $PORT/udp"
		$SUDO firewall-cmd --permanent --add-port="$PORT"/udp
		$SUDO firewall-cmd --reload
		return 0
	fi

	if command -v iptables >/dev/null 2>&1; then
		if $SUDO iptables -C INPUT -p udp --dport "$PORT" -j ACCEPT 2>/dev/null; then
			log "iptables: regra para $PORT/udp ja existe"
		else
			log "iptables: liberando $PORT/udp"
			$SUDO iptables -I INPUT -p udp --dport "$PORT" -j ACCEPT
			warn "Regra iptables nao e persistente; use iptables-persistent para manter apos reboot."
		fi
		return 0
	fi

	warn "Nenhum firewall reconhecido (ufw/firewalld/iptables). Libere $PORT/udp manualmente."
}
open_port

# Aviso: em VPS da AWS/GCP/Azure/Oracle o firewall da nuvem e separado do
# firewall do sistema. Libere $PORT/udp tambem no painel do provedor.

# --- porta ocupada ---------------------------------------------------------
# Avisa antes do bind falhar. Usa a primeira ferramenta disponivel; se nenhuma
# existir, apenas segue e deixa o proprio servidor reportar o erro.
listeners() {
	if command -v ss >/dev/null 2>&1; then
		ss -lunp 2>/dev/null
	elif command -v netstat >/dev/null 2>&1; then
		netstat -lunp 2>/dev/null
	elif command -v lsof >/dev/null 2>&1; then
		lsof -nP -iUDP:"$PORT" -sUDP:LISTEN 2>/dev/null
	fi
}

IN_USE=$(listeners | grep -E "[:.]$PORT([^0-9]|$)" || true)
if [ -n "$IN_USE" ]; then
	warn "A porta $PORT/udp ja esta em uso:"
	printf '%s\n' "$IN_USE" >&2
	warn "Encerre o processo acima antes de continuar."
fi

# --- inicio ----------------------------------------------------------------
log "Iniciando o servidor..."
exec "./$BINARY"
