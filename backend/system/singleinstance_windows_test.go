package system

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// testEventSeq — счётчик, а не время: у часов Windows слишком грубый шаг, и два
// вызова подряд получали одно и то же имя.
var testEventSeq atomic.Uint64

// testEventName даёт уникальное имя на каждый вызов: тесты не должны опознавать
// работающее приложение (или соседний прогон) как «копия уже запущена».
func testEventName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("MitM-test-instance-%d-%d", os.Getpid(), testEventSeq.Add(1))
}

// Первая заявка проходит, вторая опознаёт её и будит первую.
func TestClaimInstanceDetectsRunningCopy(t *testing.T) {
	name := testEventName(t)
	activated := make(chan struct{}, 1)

	if !claimInstance(name, func() { activated <- struct{}{} }) {
		t.Fatal("первая заявка должна была пройти")
	}
	if claimInstance(name, nil) {
		t.Fatal("вторая заявка не увидела уже занятое имя — приложение запустилось бы дважды")
	}

	select {
	case <-activated:
	case <-time.After(3 * time.Second):
		t.Fatal("работающая копия не получила просьбу показать окно")
	}
}

// Разные имена друг о друге не знают — заодно страховка от опечатки в имени.
func TestClaimInstanceIndependentNames(t *testing.T) {
	if !claimInstance(testEventName(t), nil) || !claimInstance(testEventName(t), nil) {
		t.Fatal("заявки с разными именами не должны мешать друг другу")
	}
}

// Низкая метка целостности на событии — то, ради чего вообще написан свой лок:
// без неё обычный запуск не достучится до копии, поднятой с правами администратора
// ради TUN, и молча стартует второй.
func TestInstanceEventCarriesLowIntegrityLabel(t *testing.T) {
	sa, err := instanceEventAttrs()
	if err != nil {
		t.Fatalf("собрать SECURITY_ATTRIBUTES: %v", err)
	}

	name, err := windows.UTF16PtrFromString(testEventName(t))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := windows.CreateEvent(sa, 0, 0, name)
	if err != nil {
		t.Fatalf("создать событие: %v", err)
	}
	defer windows.CloseHandle(ev)

	sd, err := windows.GetSecurityInfo(ev, windows.SE_KERNEL_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("прочитать метку целостности: %v", err)
	}
	// LW — SID «Low Mandatory Level»; NW — запрет только на запись «вверх».
	if got := sd.String(); !strings.Contains(got, "ML;;NW;;;LW") {
		t.Fatalf("на событии нет низкой метки целостности: %s", got)
	}
}
