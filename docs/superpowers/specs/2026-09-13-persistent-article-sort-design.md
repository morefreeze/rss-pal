# Persistent article sorting

Approved by the user: remember the article sort field and direction on this device, including fresh home-screen app sessions; implement and deploy to Tencent.

Read localStorage before legacy sessionStorage keys. Validate against existing options and retain published/desc defaults. Persist both initialized values and subsequent changes. Storage exceptions must not break reading. Other filters retain existing behavior. No account synchronization is required.

Verify fresh-session initial API parameters for all four combinations, migration of both legacy field keys, persistent value precedence, invalid values, and unavailable storage; run frontend checks and build. Deploy only frontend and verify remote revision, container, public assets and API health.
