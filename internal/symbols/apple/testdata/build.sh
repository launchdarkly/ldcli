#!/usr/bin/env bash
# Regenerates the checked-in golden fixture: a universal (arm64 + x86_64) Mach-O
# with DWARF, packaged as symbolsdemo.dSYM. Run on macOS with Xcode tools.
# The resulting .dSYM is committed; the test never invokes this script.
#
# We compile per-arch objects and keep them until dsymutil has collected their
# DWARF (dsymutil reads debug info from the .o files referenced by the linked
# binary's debug map), then clean up the intermediates.
set -euo pipefail
cd "$(dirname "$0")"

clang++ -g -O1 -arch arm64  -c symbolsdemo.cpp -o symbolsdemo-arm64.o
clang++ -g -O1 -arch x86_64 -c symbolsdemo.cpp -o symbolsdemo-x86_64.o
clang++ -g -arch arm64  symbolsdemo-arm64.o  -o symbolsdemo-arm64
clang++ -g -arch x86_64 symbolsdemo-x86_64.o -o symbolsdemo-x86_64
lipo -create symbolsdemo-arm64 symbolsdemo-x86_64 -o symbolsdemo

rm -rf symbolsdemo.dSYM
dsymutil symbolsdemo -o symbolsdemo.dSYM

rm -f symbolsdemo symbolsdemo-arm64 symbolsdemo-x86_64 symbolsdemo-arm64.o symbolsdemo-x86_64.o

echo "Rebuilt symbolsdemo.dSYM:"
/usr/bin/dwarfdump --uuid symbolsdemo.dSYM
echo "--- debug-info sanity (expect demo::outer / demo::inner) ---"
/usr/bin/dwarfdump --debug-info symbolsdemo.dSYM | grep -E "DW_AT_name|inlined_subroutine|subprogram" | head -30
