// Fixture source for the dSYM -> .ldsm golden test. Built into a checked-in
// universal .dSYM via build.sh; the test asserts UUID extraction, C++
// demangling, and inline-frame recovery (demo::inner inlined into demo::outer).
//
// inner() writes through a volatile pointer so the optimizer cannot fold it to a
// closed form; that keeps a real inlined body (a DW_TAG_inlined_subroutine) at a
// distinct address inside outer(), which is what the inline-chain test needs.
namespace demo {

__attribute__((always_inline)) inline int inner(volatile int* sink, int x) {
    int v = x * x + 1;
    *sink += v;
    return v;
}

__attribute__((noinline)) int outer(volatile int* sink, int n) {
    int total = 0;
    for (int i = 1; i <= n; i++) {
        total += inner(sink, i);
    }
    return total;
}

} // namespace demo

int main(int argc, char** argv) {
    volatile int sink = 0;
    return demo::outer(&sink, argc);
}
