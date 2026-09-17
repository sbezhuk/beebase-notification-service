# beebase-notification-service
 
Stores BeeBase users' push destinations and sends Firebase Cloud Messaging HTTP v1 notifications through the official Firebase Admin SDK for Go.

Production configuration is provisioned at `/opt/beebase/config/notification.env` with mode `0600`. The database is `beebase_notification`, and its password variable is exactly `POSTGRES_NOTIFICATION_PASSWORD`. Firebase credentials are supplied as base64-encoded JSON in `FIREBASE_SERVICE_ACCOUNT_JSON_BASE64`; the application decodes it in configuration/bootstrap code and never logs it. Apple variables use the subscription-service names (`APPLE_BUNDLE_ID`, `APPLE_KEY_ID`, `APPLE_ISSUER_ID`, `APPLE_PRIVATE_KEY`, `APPLE_ENVIRONMENT`) for future APNs compatibility.

Authenticated routes are `POST /api/v1/devices`, `PUT /api/v1/devices/{id}`, and `DELETE /api/v1/devices/{id}`, plus reminder CRUD at `POST|GET /api/v1/reminders` and `GET|PUT|DELETE /api/v1/reminders/{id}`. Registration is explicit and independent of login/token refresh: Flutter calls it after authentication/startup and stores the returned device UUID. It calls `PUT` when Firebase rotates the destination and `DELETE` on logout or push opt-out. Operational routes are `/health` and `/ready`.

The destination represents an FCM Installation ID (FID), using the SDK's current `Message.Fid` field (Admin SDK v4.21.0); Flutter must register the FID rather than a legacy registration token. A destination is unique and cannot be silently claimed by another user. Definitive Firebase unregistered/invalid responses trigger best-effort deletion of that destination; temporary provider failures never delete it. There is no public notification-test endpoint; delivery is driven by due reminders.

For local setup, copy `.env.example` to `.env`, set non-secret values, and derive the ignored Firebase value from the local service-account file without printing it:

```sh
jq -c . ../keys/beebase-production-4ad2b11c14cf.json | base64 | tr -d '\\n'
```

Put that output in `FIREBASE_SERVICE_ACCOUNT_JSON_BASE64` and set `FIREBASE_PROJECT_ID=beebase-production`. Start with `docker compose up --build`. Apply migrations with `make migrate-up` and deploy by building/pushing the immutable SHA and selecting it in the gateway production release workflow.

Reminder delivery uses a short PostgreSQL claim transaction with `FOR UPDATE SKIP LOCKED`; entity checks and Firebase calls happen after that transaction commits. Claims have a five-minute lease, so expired `processing` rows are recoverable after a crash. A reminder is `sent` when at least one device accepts the Firebase send. Stale devices are removed and do not defeat a successful send to another device. Temporary failures use bounded exponential backoff; permanent Firebase failures become `failed`. If Firebase accepts a push and the process crashes before PostgreSQL records `sent`, the lease may cause an unavoidable at-least-once duplicate delivery.
