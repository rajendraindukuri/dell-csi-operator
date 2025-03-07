#!/bin/bash
if [ -f "../vendor" ]; then
    # Tell the applicable Go tools to use the vendor directory, if it exists.
    MOD_FLAGS="-mod=vendor"
fi
FMT_TMPFILE=/tmp/check_fmt
FMT_COUNT_TMPFILE=${FMT_TMPFILE}.count

fmt_count() {
    if [ ! -f $FMT_COUNT_TMPFILE ]; then
        echo "0"
    fi

    head -1 $FMT_COUNT_TMPFILE
}

fmt() {
    gofmt -d ./pkg/config ./pkg/constants ./pkg/ctrlconfig ./pkg/resources ./pkg/utils | tee $FMT_TMPFILE
    cat $FMT_TMPFILE | wc -l > $FMT_COUNT_TMPFILE
    if [ ! `cat $FMT_COUNT_TMPFILE` -eq "0" ]; then
        echo Found `cat $FMT_COUNT_TMPFILE` formatting issue\(s\).
        return 1
    fi
}

echo === Checking format...
fmt
FMT_RETURN_CODE=$?
echo === Finished

echo === Vetting dell-csi-operator
CGO_ENABLED=0 go vet ${MOD_FLAGS} ./pkg/config/... ./pkg/constants/... ./pkg/ctrlconfig/... ./pkg/resources/... ./pkg/utils/...
VET_RETURN_CODE=$?
echo === Finished

echo === Linting...
(command -v golint >/dev/null 2>&1 \
    || GO111MODULE=on go install golang.org/x/lint/golint@latest) \
    && golint --set_exit_status ./pkg/config/... ./pkg/constants/... ./pkg/resources/... ./pkg/ctrlconfig/... ./pkg/utils/...
LINT_RETURN_CODE=$?
echo === Finished

# Report output.
fail_checks=0
[ "${FMT_RETURN_CODE}" != "0" ] && echo "Formatting checks failed! => Run 'make format'." && fail_checks=1
[ "${VET_RETURN_CODE}" != "0" ] && echo "Vetting checks failed!" && fail_checks=1
[ "${LINT_RETURN_CODE}" != "0" ] && echo "Linting checks failed!" && fail_checks=1

exit ${fail_checks}

