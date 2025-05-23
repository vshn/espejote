## espejote controller

Starts the controller manager

### Synopsis

Starts the controller manager

```
espejote controller [flags]
```

### Options

```
      --controller-namespace string                     The namespace the controller runs in. (default "default")
      --dynamic-admission-webhook-name string           The name of the dynamic admission webhook. (default "espejote-dynamic-webhook")
      --dynamic-admission-webhook-port int32            The port the dynamic admission webhook listens on. (default 9443)
      --dynamic-admission-webhook-service-name string   The name of the service that serves the dynamic admission webhook. (default "espejote-webhook-service")
      --enable-dynamic-admission-webhook                Enable the dynamic admission webhook. (default true)
      --health-probe-bind-address string                The address the probe endpoint binds to. (default ":8081")
  -h, --help                                            help for controller
      --jsonnet-library-namespace lib/                  The namespace to look for shared (lib/) Jsonnet libraries in. (default "default")
      --leader-elect                                    Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.
      --metrics-bind-address string                     The address the metric endpoint binds to. (default ":8080")
      --metrics-cert-key string                         The name of the metrics server key file. (default "tls.key")
      --metrics-cert-name string                        The name of the metrics server certificate file. (default "tls.crt")
      --metrics-cert-path string                        The directory that contains the metrics server certificate.
      --metrics-secure                                  If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead. (default true)
      --webhook-cert-key string                         The name of the webhook key file. (default "tls.key")
      --webhook-cert-name string                        The name of the webhook certificate file. (default "tls.crt")
      --webhook-cert-path string                        The directory that contains the webhook certificate.
      --zap-devel                                       Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn). Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error) (default true)
      --zap-encoder encoder                             Zap log encoding (one of 'json' or 'console')
      --zap-log-level level                             Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity
      --zap-stacktrace-level level                      Zap Level at and above which stacktraces are captured (one of 'info', 'error', 'panic').
      --zap-time-encoding time-encoding                 Zap time encoding (one of 'epoch', 'millis', 'nano', 'iso8601', 'rfc3339' or 'rfc3339nano'). Defaults to 'epoch'.
```

### SEE ALSO

* [espejote](espejote.md)	 - Espejote manages arbitrary resources in a Kubernetes cluster.

###### Auto generated by spf13/cobra
