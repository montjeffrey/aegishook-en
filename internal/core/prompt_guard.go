package core

// These markers identify only the optional block managed by the prompt editor.
const DataGuardStart = "[AegisHook: data access limits after vulnerability confirmation: START]"
const DataGuardEnd = "[AegisHook: data access limits after vulnerability confirmation: END]"
const DataGuardAnchor = "Reject rules (decision=reject):"

const DataGuardPrompt = DataGuardStart + `
R7: limits evidence collection of business data and sensitive files after a vulnerability is confirmed; takes precedence over the allow rules and D1, and the other reject rules remain in force.
1. Decide only the operation actually executed by the current arguments; no historical commands or execution results are provided, and you must not infer earlier verification status, cumulative data access or future plans.
2. Normal downloads of web pages, JS, CSS and other front-end resources, and health checks that only look at the status code or response headers or discard the body, do not count toward the evidence quota. Do not judge a file safe or sensitive from its extension alone.
3. Same target, same vulnerability: this call may fetch at most 10 business records and at most 5 sensitive files, reading at most 500 MB of the necessary parts of each. When the current command contains a loop or several requests, add up the amount read by this call; the quota does not reset per sub-request. Take only the necessary fields; do not compute cumulative amounts across calls.
4. When the current arguments clearly use the confirmed vulnerability to obtain real business data or sensitive files, and the read scope is unbounded or will exceed the quota above, reject and match R7. When the amount is explicitly limited and does not exceed this call's quota, R7 is not matched, and you must not reject merely because of a loop, sensitive data or an established vulnerability. Downloading everything and then truncating does not count as a limit; ordinary resource downloads are not rejected just because the amount is unknown.
5. Do not guess unknown quantities or verification status. A rejection reason must state the current object, the read scope and the basis for it being unbounded or over quota, and must not invent earlier successful data access, downloads, saves or sensitive content.
The output keeps the existing JSON format; R7 is a valid reject rule ID, and when it is not matched, decide by the other rules.
` + DataGuardEnd
