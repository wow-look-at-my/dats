<dats>
  <!-- Simple command with no inputs -->
  <test desc="echo test" cmd="echo Hello World">
    <stdout>
      <match>Hello World</match>
    </stdout>
  </test>

  <!-- Command reading from file -->
  <test desc="cat reads file" cmd="cat {inputs.input.txt}">
    <input name="input.txt">Hello, world!
</input>
    <stdout>
      <match>Hello, world!</match>
    </stdout>
  </test>

  <!-- Command reading from stdin -->
  <test desc="cat reads stdin" cmd="cat">
    <stdin>Hello from stdin</stdin>
    <stdout>
      <match>Hello from stdin</match>
    </stdout>
  </test>

  <!-- Multiple input files -->
  <test desc="concatenate two files" cmd="cat {inputs.a.txt} {inputs.b.txt} {inputs.a.txt}">
    <input name="a.txt">Line A</input>
    <input name="b.txt">Line B</input>
    <stdout>
      <match>Line A</match>
      <match>Line B</match>
    </stdout>
  </test>

  <!-- Line-specific assertions -->
  <test desc="line matching" cmd="printf &quot;line0\nline1\nline2&quot;">
    <stdout>
      <line n="0">^line0$</line>
      <line n="2">^line2$</line>
    </stdout>
  </test>

  <!-- Negative assertions -->
  <test desc="no errors in output" cmd="echo success">
    <stdout>
      <match>success</match>
      <not-match>error</not-match>
      <not-match>fail</not-match>
    </stdout>
  </test>

  <!-- Expected non-zero exit -->
  <test desc="grep returns 1 when not found" exit="1" cmd="grep -q &quot;notfound&quot;">
    <stdin>hello world</stdin>
  </test>

  <!-- Using EXIT_* variable -->
  <test desc="exit code variable" exit="EXIT_SUCCESS" cmd="true"/>
</dats>
