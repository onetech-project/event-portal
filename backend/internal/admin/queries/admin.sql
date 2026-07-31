-- name: GetAdminByEmail :one
SELECT id, email, password_hash
FROM admins
WHERE email = $1;
