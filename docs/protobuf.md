# Protobuf

The `.proto` files contains a list of fields that are populated by GoFlow2.

If the fields are changed, the schema needs to be recompiled
in order to use it.

The compilation is dependent on the language.
Keep in mind the protobuf source code and libraries changes often and this page may be outdated.

For other languages, refer to the [official guide](https://developers.google.com/protocol-buffers).

## Compile for Golang

The following two tools are required:
* [protoc](https://github.com/protocolbuffers/protobuf), a protobuf compiler, written in C
* [protoc-gen-go](https://github.com/protocolbuffers/protobuf-go), a Go plugin for protoc that can compile protobuf for Golang

The release page in the respective GitHub repositories should provide binaries distributions. Unzip/Untar if necessary.
Make sure that the two binaries are in your ``$PATH``. On Mac OS you can add the files to `/usr/local/bin` for instance.

From the root of the repository, run the following command:

```bash
$ protoc --go_opt=paths=source_relative --go_out=. pb/*.proto
```

This will compile the main protobuf schema into the `pb` directory.

You can also run the command which will also compile the protobuf for the sample enricher.

```bash
$ make proto
```