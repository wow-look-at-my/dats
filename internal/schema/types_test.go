package schema

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitCode_UnmarshalXMLAttr_Int(t *testing.T) {
	type wrapper struct {
		Exit ExitCode `xml:"exit,attr"`
	}
	var w wrapper
	require.NoError(t, xml.Unmarshal([]byte(`<wrapper exit="42"/>`), &w))
	assert.Equal(t, 42, w.Exit.IntValue())
	assert.Equal(t, "", w.Exit.VariableName())
}

func TestExitCode_UnmarshalXMLAttr_String(t *testing.T) {
	type wrapper struct {
		Exit ExitCode `xml:"exit,attr"`
	}
	var w wrapper
	require.NoError(t, xml.Unmarshal([]byte(`<wrapper exit="EXIT_SUCCESS"/>`), &w))
	assert.Equal(t, "EXIT_SUCCESS", w.Exit.VariableName())
}

func TestExitCode_UnmarshalXMLAttr_InvalidString(t *testing.T) {
	type wrapper struct {
		Exit ExitCode `xml:"exit,attr"`
	}
	invalidCodes := []string{
		"0dfsdfs",
		"abc",
		"EXIT",
		"exit_success",
		"123abc",
	}
	for _, code := range invalidCodes {
		var w wrapper
		err := xml.Unmarshal([]byte(`<wrapper exit="`+code+`"/>`), &w)
		assert.Error(t, err, "exit %q should be rejected", code)
	}
}

func TestExitCode_String(t *testing.T) {
	tests := []struct {
		name     string
		exitCode ExitCode
		want     string
	}{
		{"int zero", ExitCode("0"), "0"},
		{"int nonzero", ExitCode("127"), "127"},
		{"variable", ExitCode("EXIT_FAILURE"), "$EXIT_FAILURE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.exitCode.String())
		})
	}
}

func TestStreamCheck_Match(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><match>hello</match><match>world</match></stdout></test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))
	require.NotNil(t, tf.Test[0].Stdout)
	require.Len(t, tf.Test[0].Stdout.Match, 2)
	assert.Equal(t, []string{"hello", "world"}, tf.Test[0].Stdout.Match)
}

func TestStreamCheck_LineChecks(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><line n="0">^line0$</line><line n="2">^line2$</line></stdout></test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))
	require.NotNil(t, tf.Test[0].Stdout)
	lines := tf.Test[0].Stdout.Line
	require.Len(t, lines, 2)
	assert.Equal(t, 0, lines[0].NAttr)
	assert.Equal(t, "^line0$", lines[0].Value)
	assert.Equal(t, 2, lines[1].NAttr)
	assert.Equal(t, "^line2$", lines[1].Value)
}

func TestStreamCheck_NotMatch(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><match>hello</match><not-match>error</not-match><not-match>warning</not-match></stdout></test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))
	stdout := tf.Test[0].Stdout
	require.NotNil(t, stdout)
	assert.Equal(t, []string{"hello"}, stdout.Match)
	assert.Equal(t, []string{"error", "warning"}, stdout.NotMatch)
}

func TestFileOutput_Exists(t *testing.T) {
	input := `<dats><test cmd="echo hi">
		<output name="result.txt" exists="true"><match>ELF</match><not-match>corrupted</not-match></output>
		<output name="error.log" exists="false"/>
	</test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))

	outputs := tf.Test[0].Output
	require.Len(t, outputs, 2)

	assert.Equal(t, "result.txt", outputs[0].NameAttr)
	require.NotNil(t, outputs[0].ExistsAttr)
	assert.True(t, *outputs[0].ExistsAttr)
	assert.Equal(t, []string{"ELF"}, outputs[0].Match)
	assert.Equal(t, []string{"corrupted"}, outputs[0].NotMatch)

	assert.Equal(t, "error.log", outputs[1].NameAttr)
	require.NotNil(t, outputs[1].ExistsAttr)
	assert.False(t, *outputs[1].ExistsAttr)
}

func TestInputFile(t *testing.T) {
	input := `<dats><test cmd="cat {inputs.input.txt}">
		<input name="input.txt">Hello, world!</input>
	</test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))
	require.Len(t, tf.Test[0].Input, 1)
	assert.Equal(t, "input.txt", tf.Test[0].Input[0].NameAttr)
	assert.Equal(t, "Hello, world!", tf.Test[0].Input[0].Value)
}

func TestDats_FullParse(t *testing.T) {
	input := `<dats>
	<test desc="test one" cmd="echo hello" exit="0">
		<stdout>
			<match>hello</match>
		</stdout>
	</test>
	<test desc="test two" cmd="exit 1" exit="EXIT_FAILURE"/>
</dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))

	require.Len(t, tf.Test, 2)
	require.NotNil(t, tf.Test[0].DescAttr)
	assert.Equal(t, "test one", *tf.Test[0].DescAttr)
	require.NotNil(t, tf.Test[0].ExitAttr)
	assert.Equal(t, 0, tf.Test[0].ExitAttr.IntValue())
	require.NotNil(t, tf.Test[1].ExitAttr)
	assert.Equal(t, "EXIT_FAILURE", tf.Test[1].ExitAttr.VariableName())
}

func TestExistsAttr_NotSet(t *testing.T) {
	input := `<dats><test cmd="echo hi">
		<output name="result.txt"><match>hello</match></output>
	</test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))
	require.Len(t, tf.Test[0].Output, 1)
	assert.Nil(t, tf.Test[0].Output[0].ExistsAttr)
}

func TestStdin(t *testing.T) {
	input := `<dats><test cmd="cat"><stdin>Hello from stdin</stdin></test></dats>`
	var tf Dats
	require.NoError(t, xml.Unmarshal([]byte(input), &tf))
	require.NotNil(t, tf.Test[0].Stdin)
	assert.Equal(t, "Hello from stdin", *tf.Test[0].Stdin)
}

func TestExistsAttr_InvalidValue(t *testing.T) {
	input := `<dats><test cmd="echo hi"><output name="f.txt" exists="maybe"/></test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	assert.Error(t, err, "exists=%q should be rejected", "maybe")
}
