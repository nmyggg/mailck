#!/bin/bash

file=disposable_list.go

cat > $file <<EOF
package mailck

// DisposableDomains is a list of fake mail providers.
// The list was taken from https://github.com/andreis/disposable
// License: MIT 
// Last updated: `date`
var DisposableDomains = map[string]bool{
EOF

curl -s https://raw.githubusercontent.com/andreis/disposable-email-domains/master/domains.txt \
     | sed  's/\(.*\)/"\1": true,/' >> $file


echo >> $file
echo "}" >> $file
