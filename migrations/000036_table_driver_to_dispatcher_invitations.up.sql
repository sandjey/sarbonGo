--
-- PostgreSQL database dump
--

\restrict kLuZmL3jTUHV64PUDTbnkscUIp5foxmjpFE9giwLV03PaqMWlT0fKQHpN03g6t9

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: driver_to_dispatcher_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_to_dispatcher_invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    driver_id uuid NOT NULL,
    dispatcher_phone character varying(32) NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    responded_at timestamp with time zone,
    CONSTRAINT chk_d2d_invitations_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'accepted'::character varying, 'declined'::character varying, 'cancelled'::character varying])::text[])))
);


--
-- Name: driver_to_dispatcher_invitations driver_to_dispatcher_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_to_dispatcher_invitations
    ADD CONSTRAINT driver_to_dispatcher_invitations_pkey PRIMARY KEY (id);


--
-- Name: driver_to_dispatcher_invitations driver_to_dispatcher_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_to_dispatcher_invitations
    ADD CONSTRAINT driver_to_dispatcher_invitations_token_key UNIQUE (token);


--
-- Name: idx_d2d_invitations_dispatcher_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_d2d_invitations_dispatcher_phone ON public.driver_to_dispatcher_invitations USING btree (replace(replace(replace(TRIM(BOTH FROM dispatcher_phone), ' '::text, ''::text), '-'::text, ''::text), '+'::text, ''::text));


--
-- Name: idx_d2d_invitations_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_d2d_invitations_driver ON public.driver_to_dispatcher_invitations USING btree (driver_id);


--
-- Name: idx_d2d_invitations_driver_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_d2d_invitations_driver_status_created ON public.driver_to_dispatcher_invitations USING btree (driver_id, status, created_at DESC);


--
-- Name: idx_d2d_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_d2d_invitations_token ON public.driver_to_dispatcher_invitations USING btree (token);


--
-- Name: driver_to_dispatcher_invitations driver_to_dispatcher_invitations_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_to_dispatcher_invitations
    ADD CONSTRAINT driver_to_dispatcher_invitations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict kLuZmL3jTUHV64PUDTbnkscUIp5foxmjpFE9giwLV03PaqMWlT0fKQHpN03g6t9

