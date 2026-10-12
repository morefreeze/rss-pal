# WeChat handoff and top sharing follow-up

User reports iPhone Safari WeChat Send to Chat dismisses without a recipient picker. Existing handler synchronously shares title, text and URL and does not close the owner dialog; recipient UI belongs to the native extension. The device-level cause is unconfirmed. Reduce dedicated WeChat payload to one URL item as a bounded compatibility change, preserving full-copy and generic sharing. Do not claim recipient selection fixed without iPhone evidence.

Move the one public share toolbar before the article heading; remove only the bottom share controls and keep Read original.

Verification: regression tests assert exact WeChat payload, unchanged full copy, unique top toolbar and retained source link. Run frontend checks/build, independent review, deploy established Tencent workflow and verify live DOM and payload. Physical iPhone Send to Chat remains a required user retest.
