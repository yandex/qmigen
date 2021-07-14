This is a Go QMI protocol implementation generator.

It reads the specification from `data/*.json` and writes the implementation into
`../qmi/*.go`.

You can regenerate qmi/*.go using `go generate`.

For debugging purposes uncomment the "// DEBUG: " line in generate.go.

You need to provide QMI protocol specification in machine-readable form, as in https://github.com/freedesktop/libqmi/tree/master/data
These files will be used as an input for qmigen.

A good source of QMI information is Telit LM940 QMI Command Reference Guide: https://y1cj3stn5fbwhv73k0ipk1eg-wpengine.netdna-ssl.com/wp-content/uploads/2018/05/80545ST10798A_LM940_QMI_Command_Reference_Guide_r3.pdf
