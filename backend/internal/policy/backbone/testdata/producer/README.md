# Actual Backbone producer fixture

`workspace.bundle` preserves commit
`c77601c5fcd96188ddeca71b3cc9429a2d692a51` of an actual Copier-generated
Engineering Backbone installation. Bundle SHA-256:
`69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad`.

The reconstructed producer generated `context.json` from `request.json` with its
installed `quality-guard.py` and separately verified that receipt; both commands
returned 0. `provenance.json` records the exact producer/guard hashes, 33 receipt
documents (178,293 bytes), companion hash and original workspace revision. Its
absolute paths describe the generation environment, not required test paths.

The Go test clones only this local bundle, preserves exact document bytes and
verifies the original receipt and Git blobs. The example check argv, generator
code, and candidate Python are data; the test never executes them. This fixture
establishes producer/consumer compatibility. It contains no operator model
qualification, proves no model behavior and grants no publication authority.
