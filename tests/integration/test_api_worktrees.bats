#!/usr/bin/env bats
# test_api_worktrees.bats — Git worktree create / env seed / remove via the API + CLI.

load setup

PARENT_NAME="bats-wt-parent"
PARENT_DOMAIN="bats-wt-parent.test"
PARENT_PATH="/tmp/bats-wt-parent/${PARENT_NAME}"
WT_DOMAIN="bats-wt-parent-feature-bats.test"

@test "worktrees: git is available" {
  command -v git
}

@test "worktrees: create git parent project" {
  rm -rf /tmp/bats-wt-parent
  mkdir -p "$PARENT_PATH/public" "$PARENT_PATH/vendor"
  git -C "$PARENT_PATH" init -b main
  git -C "$PARENT_PATH" config user.email test@example.com
  git -C "$PARENT_PATH" config user.name Test
  git -C "$PARENT_PATH" config commit.gpgsign false
  printf '<?php\n' > "$PARENT_PATH/artisan"
  printf '<?php\n' > "$PARENT_PATH/public/index.php"
  printf '<?php\n' > "$PARENT_PATH/vendor/autoload.php"
  printf '{"content-hash":"abc"}\n' > "$PARENT_PATH/composer.lock"
  printf '/vendor\n.env\n' > "$PARENT_PATH/.gitignore"
  printf 'APP_URL=https://bats-wt-parent.test\nDB_DATABASE=bats\n' > "$PARENT_PATH/.env"
  printf 'app\n' > "$PARENT_PATH/README.md"
  git -C "$PARENT_PATH" add -A
  git -C "$PARENT_PATH" commit -m init
  git -C "$PARENT_PATH" branch feature/bats
  SITE_USER="${DEVCTL_SITE_USER:-testuser}"
  chown -R "$SITE_USER:$SITE_USER" /tmp/bats-wt-parent
}

@test "worktrees: POST parent site" {
  status=$(curl -s -o /tmp/wt-parent.json -w "%{http_code}" \
    -X POST -H "Content-Type: application/json" \
    -d "{\"domain\":\"${PARENT_DOMAIN}\",\"root_path\":\"${PARENT_PATH}\"}" \
    "${BASE_URL}/api/sites")
  echo "status=$status body=$(cat /tmp/wt-parent.json 2>/dev/null || true)"
  [ "$status" -eq 201 ]
}

@test "worktrees: GET config copies vendor and .env" {
  id=$(curl -sf "${BASE_URL}/api/sites" | jq -r ".[] | select(.domain==\"${PARENT_DOMAIN}\") | .id")
  body=$(curl -sf "${BASE_URL}/api/sites/${id}/worktree-config")
  echo "$body"
  echo "$body" | jq -e '.copies | index("vendor")' >/dev/null
  echo "$body" | jq -e '.copies | index(".env")' >/dev/null
  run bash -c "echo '$body' | jq -e '.symlinks | index(\"vendor\")'"
  [ "$status" -ne 0 ]
}

@test "worktrees: POST worktree returns 201" {
  id=$(curl -sf "${BASE_URL}/api/sites" | jq -r ".[] | select(.domain==\"${PARENT_DOMAIN}\") | .id")
  status=$(curl -s -o /tmp/wt-child.json -w "%{http_code}" \
    -X POST -H "Content-Type: application/json" \
    -d '{"branch":"feature/bats"}' \
    "${BASE_URL}/api/sites/${id}/worktrees")
  echo "status=$status body=$(cat /tmp/wt-child.json 2>/dev/null || true)"
  [ "$status" -eq 201 ]
}

@test "worktrees: child domain is parent-dir-branch.test" {
  domain=$(jq -r '.domain' /tmp/wt-child.json)
  [ "$domain" = "$WT_DOMAIN" ]
}

@test "worktrees: .env APP_URL rewritten" {
  root=$(jq -r '.root_path' /tmp/wt-child.json)
  grep -q "APP_URL=https://${WT_DOMAIN}" "$root/.env"
}

@test "worktrees: vendor is a real directory not a symlink" {
  root=$(jq -r '.root_path' /tmp/wt-child.json)
  [ -d "$root/vendor" ]
  [ ! -L "$root/vendor" ]
  [ -f "$root/vendor/autoload.php" ]
}

@test "worktrees: GET /worktrees lists the child" {
  id=$(curl -sf "${BASE_URL}/api/sites" | jq -r ".[] | select(.domain==\"${PARENT_DOMAIN}\") | .id")
  body=$(curl -sf "${BASE_URL}/api/sites/${id}/worktrees")
  echo "$body" | jq -r '.[].domain' | grep -q "$WT_DOMAIN"
}

@test "worktrees: CLI sites:worktrees lists the child" {
  run devctl sites:worktrees --json "${PARENT_DOMAIN}"
  echo "$output"
  [ "$status" -eq 0 ]
  echo "$output" | jq -r '.[].domain' | grep -q "$WT_DOMAIN"
}

@test "worktrees: CLI sites:get shows parent" {
  run devctl sites:get --json "${WT_DOMAIN}"
  echo "$output"
  [ "$status" -eq 0 ]
  echo "$output" | jq -e '.parent_site_id != null' >/dev/null
}

@test "worktrees: DELETE worktree removes directory" {
  parent_id=$(curl -sf "${BASE_URL}/api/sites" | jq -r ".[] | select(.domain==\"${PARENT_DOMAIN}\") | .id")
  child_id=$(jq -r '.id' /tmp/wt-child.json)
  root=$(jq -r '.root_path' /tmp/wt-child.json)
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -X DELETE "${BASE_URL}/api/sites/${parent_id}/worktrees/${child_id}")
  [[ "$status" -eq 204 || "$status" -eq 200 ]]
  [ ! -e "$root" ]
}

@test "worktrees: DELETE parent site" {
  id=$(curl -sf "${BASE_URL}/api/sites" | jq -r ".[] | select(.domain==\"${PARENT_DOMAIN}\") | .id")
  status=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${BASE_URL}/api/sites/${id}")
  [[ "$status" -eq 200 || "$status" -eq 204 ]]
  rm -rf /tmp/bats-wt-parent
}