# synthos-bot

The Synthos Collective Telegram bot. It only **reads** the network: it has no
wallet and cannot send SYN.

- **Watchdog.** Messages your alert chat when blocks stop for 3 minutes, when
  a validator hasn't signed for 5 minutes, when the server's own node can't be
  read for 5 minutes, and when a new outside node passes 20 verified
  heartbeats. One message when something goes wrong, one when it recovers.
- **Node helper.** `/node <node id>` shows a node's uptime and reward status.
- **Chain Q&A.** `/status`, `/validators`, `/nodes`, `/balance <0x address>`.
  Plain-language questions are answered by Claude using live chain data, if
  you give it an Anthropic API key (optional).

## Set it up (on the Salt Lake City server)

1. **Create the bot.** In Telegram, message **@BotFather**, send `/newbot`,
   and follow the steps. It gives you a token. Keep it secret.
2. **Install it:**

   ```sh
   useradd --system --home /var/lib/synthos-bot --create-home synthos-bot
   install -m 755 synthos-bot-linux-amd64 /usr/local/bin/synthos-bot
   mkdir -p /etc/synthos-bot && chmod 700 /etc/synthos-bot
   nano /etc/synthos-bot/telegram-token          # paste the BotFather token
   chmod 600 /etc/synthos-bot/telegram-token
   chown -R synthos-bot /etc/synthos-bot
   ```

3. **Find your chat ID.** Message your new bot once (say "hi"), then on the
   server run (it prints your chat ID; the token stays on the server):

   ```sh
   curl -s "https://api.telegram.org/bot$(cat /etc/synthos-bot/telegram-token)/getUpdates" | grep -o '"chat":{"id":[0-9-]*' | head -1
   ```

   Optional, for plain-language answers: put an Anthropic API key in
   `/etc/synthos-bot/anthropic-key` (also `chmod 600`) and set
   `SYNTHOS_BOT_MODEL` in the service file to the Claude model you want.
   Answers are capped at 10 per chat and 100 in total per hour.

4. **Run it as a service:** copy `deploy/synthos-bot/synthos-bot.service` to
   `/etc/systemd/system/`, put your chat ID and home IP into its `ExecStart`
   line, then:

   ```sh
   systemctl daemon-reload
   systemctl enable --now synthos-bot
   journalctl -u synthos-bot -f
   ```

   The bot says "Synthos Collective watchdog is on" in your chat when it starts.

## Settings

| Flag | Default | Meaning |
|---|---|---|
| `-alert-chat` | (required) | Telegram chat that gets alerts |
| `-rpc` | `http://127.0.0.1:26657` | CometBFT RPC of the server's synthos-comet |
| `-chain-api` | `http://127.0.0.1:8080` | that node's `--api` address (balances) |
| `-registry` | `https://82-29-54-7.sslip.io` | node registry |
| `-halt-after` | `3m` | no block for this long = stopped |
| `-miss-for` | `5m` | validator not signing for this long = offline |
| `-min-heartbeats` | `20` | outside node must prove this much first |
| `-home-ips` | (none) | your own machines' IPs, never counted as outside |
| `-state` | `synthos-bot-state.json` | remembers announced outside nodes |

Secrets are read only from files named in `SYNTHOS_BOT_TELEGRAM_TOKEN_FILE`
and `SYNTHOS_BOT_ANTHROPIC_KEY_FILE`. The bot refuses a secret file other
users can read, and never logs a token.
