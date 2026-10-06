---
title: Auth
description: Email and password sign-in for your app's users, with JWT sessions.
---

One auth service per project. In the browser, use the publishable key:

```ts
const client = createClient(projectUrl, 'sbp_...', { storage: localStorage })
await client.auth.signUp({ email, password, data: { plan: 'free' } })
await client.auth.signInWithPassword({ email, password })
const user = await client.auth.getUser()
const token = await client.auth.getAccessToken() // send to your server
client.auth.onAuthStateChange((event) => console.log(event))
```

On your server, verify the access token with the JWT secret from the service's Overview tab. Tokens are HS256 JWTs with `sub` (user id), `email`, `exp`, `user_metadata` and `app_metadata`.

## Sessions

Access tokens last one hour by default; the SDK refreshes them before they expire. Refresh tokens rotate on every use, and reusing an old one signs that session out everywhere.

## Recovery and passwordless

`recover({ email })` emails a one-time link and 6-digit code; `verifyOtp` turns either into a session, then `updateUser({ password })` sets a new password. `signInWithOtp` does the same for passwordless sign-in. Links only go to your **site URL** or an allowlisted redirect (Users → Settings). Without SMTP, call `auth.admin.generateLink` from your server and send the email yourself.

## Admin

With the secret key: `auth.admin.listUsers`, `createUser`, `updateUser` (including `banned`), `deleteUser`, `getSettings`, `updateSettings`.
