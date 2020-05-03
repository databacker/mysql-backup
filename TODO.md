# Golang TODO

* parsing non-transferred cmdline options

## Non-transferred cmdline options

* `AWS_CLI_OPTS`: Additional arguments to be passed to the `aws` part of the `aws s3 cp` command, click [here](https://docs.aws.amazon.com/cli/latest/reference/#options) for a list. _Be careful_, as you can break something!
* `AWS_CLI_S3_CP_OPTS`: Additional arguments to be passed to the `s3 cp` part of the `aws s3 cp` command, click [here](https://docs.aws.amazon.com/cli/latest/reference/s3/cp.html#options) for a list. If you are using AWS KMS, `sse`, `sse-kms-key-id`, etc., may be of interest.
