#!/bin/sh
# Puts Part 12's files on the remote box. Every lesson load runs it
# (setup.run), because checkpoints cover the workspace, not the box.
set -eu
ssh box 'rm -rf app greeter site'
tar -C box -cf - app greeter site | ssh box 'tar -xf -'
ssh box 'set -eu
cd site
git init -q -b main
git config user.name Deploy
git config user.email deploy@box
git add -A
GIT_AUTHOR_DATE=2026-01-05T10:00:00 GIT_COMMITTER_DATE=2026-01-05T10:00:00 \
  git commit -q -m "First version of the site"
sudo install -d -m 755 -o root -g root /etc/app
printf "MODE=staging\nWORKERS=2\n" | sudo tee /etc/app/app.env >/dev/null
sudo chmod 644 /etc/app/app.env
sudo install -d -m 750 -o www-data -g www-data /srv/sidecar
printf "status: starting\nchecked-by: nobody\n" | sudo -u www-data tee /srv/sidecar/health.txt >/dev/null'
