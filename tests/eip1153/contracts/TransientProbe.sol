// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

contract TransientProbe {
    error Boom();

    event Loaded(uint256 indexed key, uint256 value);


    function write(uint256 k, uint256 v) external {
        assembly {
            tstore(k, v)
        }
    }


    function read(uint256 k) external view returns (uint256 r) {
        assembly {
            r := tload(k)
        }
    }

    function readToEvent(uint256 k) external {
        uint256 r;
        assembly {
            r := tload(k)
        }
        emit Loaded(k, r);
    }


    function writeThenReadToEvent(uint256 k, uint256 v) external {
        uint256 r;
        assembly {
            tstore(k, v)
            r := tload(k)
        }
        emit Loaded(k, r);
    }

    function callThenStaticWrite(address target, uint256 k, uint256 v)
        external
        returns (bool callOk, bool staticOk, uint256 staticRetLen)
    {
        bytes memory payload = abi.encodeWithSelector(this.write.selector, k, v);

        (callOk, ) = target.call(payload);

        (staticOk, ) = target.staticcall(payload);
        assembly {
            staticRetLen := returndatasize()
        }
    }

    function writeThenStaticRead(address target, uint256 k, uint256 v)
        external
        returns (bool ok, uint256 value)
    {
        TransientProbe(target).write(k, v);

        bytes memory ret;
        (ok, ret) = target.staticcall(abi.encodeWithSelector(this.read.selector, k));
        if (ok && ret.length == 32) {
            value = abi.decode(ret, (uint256));
        }
    }


    function writeThenRevert(uint256 k, uint256 v) external {
        assembly {
            tstore(k, v)
        }
        revert Boom();
    }

    function rollbackAfterInnerRevert(uint256 k, uint256 pre, uint256 inner)
        external
        returns (uint256 r)
    {
        assembly {
            tstore(k, pre)
        }
        (bool ok, ) = address(this).call(
            abi.encodeWithSelector(this.writeThenRevert.selector, k, inner)
        );
        require(!ok, "inner call was expected to revert");
        assembly {
            r := tload(k)
        }
    }

    function writeCallWriteThenRevert(uint256 k, uint256 a, uint256 b) external {
        assembly {
            tstore(k, a)
        }
        TransientProbe(address(this)).write(k, b);
        revert Boom();
    }


    function rollbackIncludesInnerCallWrites(uint256 k, uint256 pre, uint256 a, uint256 b)
        external
        returns (uint256 r)
    {
        assembly {
            tstore(k, pre)
        }
        (bool ok, ) = address(this).call(
            abi.encodeWithSelector(this.writeCallWriteThenRevert.selector, k, a, b)
        );
        require(!ok, "inner call was expected to revert");
        assembly {
            r := tload(k)
        }
    }


    function writeThenCallOtherWrite(address other, uint256 k, uint256 mine, uint256 theirs)
        external
        returns (uint256 ourValue, uint256 theirValue)
    {
        assembly {
            tstore(k, mine)
        }
        TransientProbe(other).write(k, theirs);
        assembly {
            ourValue := tload(k)
        }
        theirValue = TransientProbe(other).read(k);
    }


    function delegateWrite(address impl, uint256 k, uint256 v)
        external
        returns (uint256 ourValue, uint256 implValue)
    {
        (bool ok, ) = impl.delegatecall(abi.encodeWithSelector(this.write.selector, k, v));
        require(ok, "delegatecall failed");
        assembly {
            ourValue := tload(k)
        }
        implValue = TransientProbe(impl).read(k);
    }


    function readFrom(address origin, uint256 k) external view returns (uint256) {
        return TransientProbe(origin).read(k);
    }


    function reentrantRead(address other, uint256 k, uint256 v)
        external
        returns (uint256 seen)
    {
        assembly {
            tstore(k, v)
        }
        seen = TransientProbe(other).readFrom(address(this), k);
    }

    function innerWritePersists(uint256 k, uint256 v) external returns (uint256 r) {
        TransientProbe(address(this)).write(k, v);
        assembly {
            r := tload(k)
        }
    }
}
